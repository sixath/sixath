package cron

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	agentv1 "backend/api/agent/v1"
	"backend/internal/biz"
	"backend/internal/channel"
	"backend/internal/chat"
	"backend/internal/conf"
	"backend/internal/service"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/sixath/framework/tool"
)

// Executor 执行定时任务：agent_turn / skill_execute，并投递结果
type Executor struct {
	agentSvc               *service.AgentService
	chatUC                 *biz.ChatUsecase
	cronUC                 *biz.CronUsecase
	channelUC              *biz.ChannelUsecase
	log                    *log.Helper
	servicePrincipalUserID string
}

// NewExecutor 创建 CronExecutor
func NewExecutor(agentSvc *service.AgentService, chatUC *biz.ChatUsecase, cronUC *biz.CronUsecase, channelUC *biz.ChannelUsecase, auth *conf.Auth, logger log.Logger) *Executor {
	e := &Executor{
		agentSvc:               agentSvc,
		chatUC:                 chatUC,
		cronUC:                 cronUC,
		channelUC:              channelUC,
		log:                    log.NewHelper(logger),
		servicePrincipalUserID: servicePrincipalUserID(auth),
	}
	chat.SetCronClient(chat.NewPortalCronClient(cronUC, e.Execute))
	return e
}

// Execute 立即执行任务（手动触发，可异步调用），不改变任务的 next_run_at。
func (e *Executor) Execute(ctx context.Context, task *biz.CronTaskMeta) {
	e.executeAt(ctx, task, time.Now())
}

// executeAt 执行一次计划在 triggeredAt 的运行；调度器在领取该时间点后调用。
func (e *Executor) executeAt(ctx context.Context, task *biz.CronTaskMeta, triggeredAt time.Time) {
	ctx = e.internalContext(ctx)
	runID := uuid.New().String()

	run := &biz.CronRunMeta{
		ID:          runID,
		TaskID:      task.ID,
		TriggeredAt: triggeredAt,
		StartedAt:   time.Now(),
		Status:      "running",
	}
	if err := e.cronUC.CreateRun(ctx, run); err != nil {
		e.log.Errorf("create cron run failed: %v", err)
		return
	}

	// 带超时的 context
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(task.TimeoutSec)*time.Second)
	defer cancel()

	var output string
	var execErr error

	switch task.PayloadKind {
	case "agent_turn":
		output, execErr = e.runAgentTurn(runCtx, task)
	case "skill_execute":
		output, execErr = e.runSkillExecute(runCtx, task)
	default:
		execErr = fmt.Errorf("unknown payload_kind: %s", task.PayloadKind)
	}

	finishedAt := time.Now()
	status := "success"
	if execErr != nil {
		status = "failed"
		output = execErr.Error()
	}

	summary := outputSummary(output)

	updates := map[string]any{
		"finished_at":    finishedAt,
		"status":         status,
		"output_summary": summary,
		"error":          "",
	}
	if execErr != nil {
		updates["error"] = execErr.Error()
	}

	// 投递
	if task.DeliveryMode != "none" && task.DeliveryMode != "" {
		deliveryOK := e.deliver(ctx, task, runID, status, summary, output, execErr, triggeredAt, finishedAt)
		updates["delivery_ok"] = deliveryOK
		if !deliveryOK && !task.DeliveryBestEffort {
			updates["status"] = "failed"
		}
	}

	if err := e.cronUC.UpdateRun(ctx, runID, updates); err != nil {
		e.log.Errorf("update cron run failed: %v", err)
	}
}

// maxOutputSummaryBytes bounds the stored and webhook-delivered output summary.
const maxOutputSummaryBytes = 2000

// outputSummary truncates output on a UTF-8 boundary; MySQL rejects a cut multi-byte character.
func outputSummary(output string) string {
	if len(output) <= maxOutputSummaryBytes {
		return output
	}
	b := output[:maxOutputSummaryBytes]
	for len(b) > 0 && !utf8.ValidString(b) {
		b = b[:len(b)-1]
	}
	return b + "..."
}

func (e *Executor) internalContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return biz.WithCallerUserID(ctx, e.servicePrincipalUserID)
}

func servicePrincipalUserID(auth *conf.Auth) string {
	if auth == nil {
		return "bootstrap"
	}
	if userID := strings.TrimSpace(auth.GetServicePrincipalUserId()); userID != "" {
		return userID
	}
	if userID := strings.TrimSpace(auth.GetBootstrapUserId()); userID != "" {
		return userID
	}
	return "bootstrap"
}

func (e *Executor) runAgentTurn(ctx context.Context, task *biz.CronTaskMeta) (string, error) {
	runCtx := chat.WithCronSessionContext(ctx)
	runCtx = context.WithValue(runCtx, tool.ContextKeyAgentID, task.AgentID)
	req := &agentv1.ChatRequest{
		Id:      task.AgentID,
		Content: task.PayloadContent,
	}
	reply, err := e.agentSvc.Chat(runCtx, req)
	if err != nil {
		return "", err
	}
	if reply.GetRet() != nil && reply.GetRet().GetCode() != 0 {
		return "", fmt.Errorf("agent chat: %s", reply.GetRet().GetMessage())
	}
	return reply.GetContent(), nil
}

func (e *Executor) runSkillExecute(ctx context.Context, task *biz.CronTaskMeta) (string, error) {
	// payload_content 格式: skill-name/scripts/run.sh
	path := strings.TrimSpace(task.PayloadContent)
	if path == "" {
		return "", fmt.Errorf("payload_content is required for skill_execute")
	}
	req := &agentv1.ExecuteSkillRequest{
		Id:    task.AgentID,
		Path:  path,
		Input: "",
	}
	reply, err := e.agentSvc.ExecuteSkill(ctx, req)
	if err != nil {
		return "", err
	}
	if reply.GetRet() != nil && reply.GetRet().GetCode() != 0 {
		return "", fmt.Errorf("skill execute: %s", reply.GetRet().GetMessage())
	}
	return reply.GetOutput(), nil
}

func (e *Executor) deliver(ctx context.Context, task *biz.CronTaskMeta, runID, status, summary, output string, execErr error, triggeredAt, finishedAt time.Time) bool {
	switch task.DeliveryMode {
	case "webhook":
		return e.deliverWebhook(ctx, task, runID, status, output, execErr, triggeredAt, finishedAt)
	case "session":
		return e.deliverSession(ctx, task, summary, execErr)
	case "channel":
		return e.deliverChannel(ctx, task, summary, execErr)
	}
	return true
}

// webhookTimeout bounds one generic webhook delivery.
const webhookTimeout = 10 * time.Second

// isWeComRobotWebhook reports whether url is a WeCom group robot, which only accepts its own
// message format and reports errors in the body of a 200 response.
func isWeComRobotWebhook(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && strings.HasSuffix(u.Path, "/cgi-bin/webhook/send")
}

func (e *Executor) deliverWebhook(ctx context.Context, task *biz.CronTaskMeta, runID, status, output string, execErr error, triggeredAt, finishedAt time.Time) bool {
	if task.DeliveryWebhookURL == "" {
		return false
	}
	if isWeComRobotWebhook(task.DeliveryWebhookURL) {
		content := output
		if execErr != nil {
			content = "定时任务「" + task.Name + "」执行失败：" + execErr.Error()
		}
		if err := channel.PushToWeCom(ctx, task.DeliveryWebhookURL, content, "markdown"); err != nil {
			e.log.Errorf("wecom webhook delivery failed: %v", err)
			return false
		}
		return true
	}
	body := map[string]any{
		"task_id":        task.ID,
		"task_name":      task.Name,
		"run_id":         runID,
		"status":         status,
		"output_summary": outputSummary(output),
		"triggered_at":   triggeredAt.Format(time.RFC3339),
		"finished_at":    finishedAt.Format(time.RFC3339),
	}
	if execErr != nil {
		body["error"] = execErr.Error()
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		e.log.Errorf("marshal webhook body: %v", err)
		return false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, task.DeliveryWebhookURL, bytes.NewReader(jsonBody))
	if err != nil {
		e.log.Errorf("create webhook request: %v", err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	if task.DeliverySecret != "" {
		mac := hmac.New(sha256.New, []byte(task.DeliverySecret))
		mac.Write(jsonBody)
		req.Header.Set("X-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := (&http.Client{Timeout: webhookTimeout}).Do(req)
	if err != nil {
		e.log.Errorf("webhook POST failed: %v", err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e.log.Errorf("webhook returned %d", resp.StatusCode)
		return false
	}
	return true
}

func (e *Executor) deliverChannel(ctx context.Context, task *biz.CronTaskMeta, outputSummary string, execErr error) bool {
	if task.DeliveryChannelID == "" {
		return false
	}
	ch, err := e.channelUC.Get(ctx, task.DeliveryChannelID)
	if err != nil {
		e.log.Errorf("get channel for delivery: %v", err)
		return false
	}
	content := outputSummary
	if execErr != nil {
		content = "[定时任务 " + task.Name + "] 执行失败: " + execErr.Error()
	} else {
		content = "[定时任务 " + task.Name + "] " + content
	}
	switch ch.Type {
	case "wxpusher":
		if ch.AppToken == "" || len(ch.DefaultUids) == 0 {
			e.log.Errorf("wxpusher channel %s missing app_token or default_uids", ch.ID)
			return false
		}
		summary := task.Name
		if execErr != nil {
			summary = task.Name + " 失败"
		}
		if err := channel.PushToWxPusher(ctx, ch.AppToken, ch.DefaultUids, content, summary); err != nil {
			e.log.Errorf("wxpusher push failed: %v", err)
			return false
		}
		return true
	case "wecom":
		if ch.WebhookURL == "" {
			e.log.Errorf("wecom channel %s missing webhook_url", ch.ID)
			return false
		}
		if err := channel.PushToWeCom(ctx, ch.WebhookURL, content, "text"); err != nil {
			e.log.Errorf("wecom push failed: %v", err)
			return false
		}
		return true
	default:
		e.log.Errorf("channel %s type %s does not support delivery (supported: wxpusher|wecom)", ch.ID, ch.Type)
		return false
	}
}

func (e *Executor) deliverSession(ctx context.Context, task *biz.CronTaskMeta, outputSummary string, execErr error) bool {
	if task.DeliverySessionID == "" {
		return false
	}
	if _, err := e.chatUC.GetSession(ctx, task.DeliverySessionID); err != nil {
		e.log.Errorf("get session for delivery: %v", err)
		return false
	}
	content := outputSummary
	if execErr != nil {
		content = "执行失败: " + execErr.Error()
	}
	_, err := e.chatUC.CreateMessage(ctx, task.DeliverySessionID, "assistant", "[定时任务 "+task.Name+"] "+content)
	if err != nil {
		e.log.Errorf("create message in session: %v", err)
		return false
	}
	return true
}
