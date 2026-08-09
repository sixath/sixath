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
	"strconv"
	"strings"
	"time"

	agentv1 "backend/api/agent/v1"
	"backend/internal/biz"
	"backend/internal/channel"
	"backend/internal/chat"
	"backend/internal/conf"
	"backend/internal/service"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
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

// Execute 执行任务（可异步调用）
func (e *Executor) Execute(ctx context.Context, task *biz.CronTaskMeta) {
	ctx = e.internalContext(ctx)
	runID := uuid.New().String()
	triggeredAt := time.Now()
	if task.NextRunAt != nil {
		triggeredAt = *task.NextRunAt
	}

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

	// 截断 output 作为 summary
	summary := output
	if len(summary) > 2000 {
		summary = summary[:2000] + "..."
	}

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
		deliveryOK := e.deliver(ctx, task, runID, status, summary, execErr, triggeredAt, finishedAt)
		updates["delivery_ok"] = deliveryOK
		if !deliveryOK && !task.DeliveryBestEffort {
			updates["status"] = "failed"
		}
	}

	if err := e.cronUC.UpdateRun(ctx, runID, updates); err != nil {
		e.log.Errorf("update cron run failed: %v", err)
	}

	// 更新 next_run_at（at 类型一次性任务不再更新）
	if task.ScheduleKind != "at" {
		next := e.computeNextRun(task, finishedAt)
		if next != nil {
			_ = e.cronUC.UpdateNextRun(ctx, task.ID, *next)
		}
	}
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

func (e *Executor) deliver(ctx context.Context, task *biz.CronTaskMeta, runID, status, outputSummary string, execErr error, triggeredAt, finishedAt time.Time) bool {
	switch task.DeliveryMode {
	case "webhook":
		return e.deliverWebhook(ctx, task, runID, status, outputSummary, execErr, triggeredAt, finishedAt)
	case "session":
		return e.deliverSession(ctx, task, outputSummary, execErr)
	case "channel":
		return e.deliverChannel(ctx, task, outputSummary, execErr)
	}
	return true
}

func (e *Executor) deliverWebhook(ctx context.Context, task *biz.CronTaskMeta, runID, status, outputSummary string, execErr error, triggeredAt, finishedAt time.Time) bool {
	if task.DeliveryWebhookURL == "" {
		return false
	}
	body := map[string]any{
		"task_id":        task.ID,
		"task_name":      task.Name,
		"run_id":         runID,
		"status":         status,
		"output_summary": outputSummary,
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

	resp, err := http.DefaultClient.Do(req)
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

func (e *Executor) computeNextRun(task *biz.CronTaskMeta, after time.Time) *time.Time {
	loc := time.UTC
	if task.Timezone != "" {
		if l, err := time.LoadLocation(task.Timezone); err == nil {
			loc = l
		}
	}
	now := after.In(loc)

	switch task.ScheduleKind {
	case "cron":
		if task.ScheduleExpr == "" {
			return nil
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		sched, err := parser.Parse(task.ScheduleExpr)
		if err != nil {
			return nil
		}
		next := sched.Next(now)
		return &next
	case "every":
		sec := 3600
		if n, err := strconv.Atoi(task.ScheduleExpr); err == nil && n > 0 {
			sec = n
		}
		next := now.Add(time.Duration(sec) * time.Second)
		return &next
	}
	return nil
}
