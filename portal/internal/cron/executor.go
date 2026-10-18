package cron

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime/debug"
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
	ilink                  IlinkPusher
	// payloadRunner replaces the payload dispatch when set (tests).
	payloadRunner func(ctx context.Context, task *biz.CronTaskMeta) (string, error)
}

// IlinkPusher sends text to a weixin_ilink roster peer (*biz.WeixinIlinkUsecase satisfies it).
type IlinkPusher interface {
	Push(ctx context.Context, channelUUID, peerID, text string) ([]string, error)
}

// SetIlinkPusher enables delivery to weixin_ilink channels; a typed-nil usecase disables it.
func (e *Executor) SetIlinkPusher(p IlinkPusher) {
	if uc, ok := p.(*biz.WeixinIlinkUsecase); ok && uc == nil {
		p = nil
	}
	e.ilink = p
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
	e.executeAt(ctx, task, biz.CronTriggerManual, time.Now())
}

// executeScheduled 执行调度器已领取的、计划在 triggeredAt 的运行。
func (e *Executor) executeScheduled(ctx context.Context, task *biz.CronTaskMeta, triggeredAt time.Time) {
	e.executeAt(ctx, task, biz.CronTriggerSchedule, triggeredAt)
}

func (e *Executor) executeAt(ctx context.Context, task *biz.CronTaskMeta, trigger string, triggeredAt time.Time) {
	ctx = e.internalContext(ctx)
	runID := uuid.New().String()

	run := &biz.CronRunMeta{
		ID:          runID,
		TaskID:      task.ID,
		Trigger:     trigger,
		TriggeredAt: triggeredAt,
		StartedAt:   time.Now(),
		Status:      biz.CronRunRunning,
	}
	if err := e.cronUC.CreateRun(ctx, run); err != nil {
		e.log.Errorf("create cron run for task %s failed, run not recorded: %v", task.ID, err)
		return
	}

	output, execErr := e.runPayload(ctx, task)

	finishedAt := time.Now()
	status := biz.CronRunSuccess
	if execErr != nil {
		status = biz.CronRunFailed
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

	if task.DeliveryMode != "none" && task.DeliveryMode != "" {
		deliveryErr := e.deliver(ctx, task, runID, status, summary, output, execErr, triggeredAt, finishedAt)
		updates["delivery_ok"] = deliveryErr == nil
		if deliveryErr != nil {
			e.log.Errorf("cron task %s run %s delivery failed: %v", task.ID, runID, deliveryErr)
			updates["delivery_error"] = deliveryErr.Error()
			if !task.DeliveryBestEffort {
				updates["status"] = biz.CronRunFailed
			}
		}
	}

	e.saveResult(ctx, runID, updates)
}

// runPayload runs the task under its timeout and turns a panic into an error so the run
// still gets a result.
func (e *Executor) runPayload(ctx context.Context, task *biz.CronTaskMeta) (output string, err error) {
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(task.TimeoutSec)*time.Second)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			e.log.Errorf("cron task %s panicked: %v\n%s", task.ID, r, debug.Stack())
			output, err = "", fmt.Errorf("执行异常（panic）：%v", r)
		}
	}()
	if e.payloadRunner != nil {
		return e.payloadRunner(runCtx, task)
	}
	switch task.PayloadKind {
	case "agent_turn":
		return e.runAgentTurn(runCtx, task)
	case "skill_execute":
		return e.runSkillExecute(runCtx, task)
	default:
		return "", fmt.Errorf("unknown payload_kind: %s", task.PayloadKind)
	}
}

// saveResultAttempts bounds retries of the result write before falling back.
const saveResultAttempts = 3

var saveResultBackoff = time.Second

// saveResult writes the run result, retrying; if the full write keeps failing it writes only
// status and error so the run does not stay "running" with no explanation.
func (e *Executor) saveResult(ctx context.Context, runID string, updates map[string]any) {
	var err error
	for i := 0; i < saveResultAttempts; i++ {
		if i > 0 {
			time.Sleep(saveResultBackoff)
		}
		if err = e.cronUC.UpdateRun(ctx, runID, updates); err == nil {
			return
		}
		e.log.Errorf("update cron run %s failed (attempt %d): %v", runID, i+1, err)
	}
	msg := fmt.Sprintf("保存执行结果失败：%v", err)
	if orig, _ := updates["error"].(string); orig != "" {
		msg += "；执行错误：" + orig
	}
	fallback := map[string]any{
		"finished_at": updates["finished_at"],
		"status":      updates["status"],
		"error":       outputSummary(msg),
	}
	if err := e.cronUC.UpdateRun(ctx, runID, fallback); err != nil {
		e.log.Errorf("update cron run %s fallback failed: %v", runID, err)
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

func (e *Executor) deliver(ctx context.Context, task *biz.CronTaskMeta, runID, status, summary, output string, execErr error, triggeredAt, finishedAt time.Time) error {
	switch task.DeliveryMode {
	case "webhook":
		return e.deliverWebhook(ctx, task, runID, status, output, execErr, triggeredAt, finishedAt)
	case "session":
		return e.deliverSession(ctx, task, summary, execErr)
	case "channel":
		return e.deliverChannel(ctx, task, summary, execErr)
	}
	return fmt.Errorf("未知投递方式：%s", task.DeliveryMode)
}

// webhookTimeout bounds one generic webhook delivery.
const webhookTimeout = 10 * time.Second

// isWeComRobotWebhook reports whether url is a WeCom group robot, which only accepts its own
// message format and reports errors in the body of a 200 response.
func isWeComRobotWebhook(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && strings.HasSuffix(u.Path, "/cgi-bin/webhook/send")
}

func (e *Executor) deliverWebhook(ctx context.Context, task *biz.CronTaskMeta, runID, status, output string, execErr error, triggeredAt, finishedAt time.Time) error {
	if task.DeliveryWebhookURL == "" {
		return errors.New("未配置 webhook 地址")
	}
	if isWeComRobotWebhook(task.DeliveryWebhookURL) {
		content := output
		if execErr != nil {
			content = "定时任务「" + task.Name + "」执行失败：" + execErr.Error()
		}
		if strings.TrimSpace(content) == "" {
			content = "定时任务「" + task.Name + "」执行完成，无输出"
		}
		if err := channel.PushToWeCom(ctx, task.DeliveryWebhookURL, content, "markdown"); err != nil {
			return fmt.Errorf("企业微信机器人推送失败：%w", scrubURL(err, task.DeliveryWebhookURL))
		}
		return nil
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
		return fmt.Errorf("构造 webhook 请求体失败：%w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, task.DeliveryWebhookURL, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("构造 webhook 请求失败：%w", scrubURL(err, task.DeliveryWebhookURL))
	}
	req.Header.Set("Content-Type", "application/json")
	if task.DeliverySecret != "" {
		mac := hmac.New(sha256.New, []byte(task.DeliverySecret))
		mac.Write(jsonBody)
		req.Header.Set("X-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := (&http.Client{Timeout: webhookTimeout}).Do(req)
	if err != nil {
		return fmt.Errorf("webhook 请求失败：%w", scrubURL(err, task.DeliveryWebhookURL))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook 返回 HTTP %d", resp.StatusCode)
	}
	return nil
}

// redactURL drops query, fragment and userinfo, where webhook keys live.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<webhook>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// scrubURL removes raw (and its parsed form) from err's message; delivery errors are stored
// and shown in the UI.
func scrubURL(err error, raw string) error {
	if err == nil || raw == "" {
		return err
	}
	msg := err.Error()
	redacted := redactURL(raw)
	for _, s := range []string{raw, parsedURLString(raw)} {
		if s != "" && s != redacted {
			msg = strings.ReplaceAll(msg, s, redacted)
		}
	}
	if msg == err.Error() {
		return err
	}
	return errors.New(msg)
}

func parsedURLString(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.String()
}

func (e *Executor) deliverChannel(ctx context.Context, task *biz.CronTaskMeta, outputSummary string, execErr error) error {
	if task.DeliveryChannelID == "" {
		return errors.New("未配置投递渠道")
	}
	ch, err := e.channelUC.Get(ctx, task.DeliveryChannelID)
	if err != nil {
		return fmt.Errorf("获取投递渠道 %s 失败：%w", task.DeliveryChannelID, err)
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
			return fmt.Errorf("WxPusher 渠道 %s 缺少 app_token 或 default_uids", ch.ID)
		}
		summary := task.Name
		if execErr != nil {
			summary = task.Name + " 失败"
		}
		if err := channel.PushToWxPusher(ctx, ch.AppToken, ch.DefaultUids, content, summary); err != nil {
			return fmt.Errorf("WxPusher 推送失败：%w", err)
		}
		return nil
	case "wecom":
		if ch.WebhookURL == "" {
			return fmt.Errorf("企业微信渠道 %s 缺少 webhook_url", ch.ID)
		}
		if err := channel.PushToWeCom(ctx, ch.WebhookURL, content, "text"); err != nil {
			return fmt.Errorf("企业微信推送失败：%w", scrubURL(err, ch.WebhookURL))
		}
		return nil
	case biz.ChannelTypeWeixinIlink:
		if e.ilink == nil {
			return errors.New("微信渠道推送未启用")
		}
		if task.DeliveryPeerID == "" {
			return fmt.Errorf("微信渠道 %s 未配置接收人（delivery_peer_id）", ch.ChannelID)
		}
		if _, err := e.ilink.Push(ctx, ch.ID, task.DeliveryPeerID, content); err != nil {
			return fmt.Errorf("微信推送失败：%w", err)
		}
		return nil
	default:
		return fmt.Errorf("渠道 %s 的类型 %s 不支持投递（仅支持 wxpusher、wecom、weixin_ilink）", ch.ID, ch.Type)
	}
}

func (e *Executor) deliverSession(ctx context.Context, task *biz.CronTaskMeta, outputSummary string, execErr error) error {
	if task.DeliverySessionID == "" {
		return errors.New("未配置投递会话")
	}
	if _, err := e.chatUC.GetSession(ctx, task.DeliverySessionID); err != nil {
		return fmt.Errorf("获取投递会话 %s 失败：%w", task.DeliverySessionID, err)
	}
	content := outputSummary
	if execErr != nil {
		content = "执行失败: " + execErr.Error()
	}
	if _, err := e.chatUC.CreateMessage(ctx, task.DeliverySessionID, "assistant", "[定时任务 "+task.Name+"] "+content); err != nil {
		return fmt.Errorf("写入会话消息失败：%w", err)
	}
	return nil
}
