package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/redact"
)

// 模型错误的两层容错：
//
//  1. model.WrapResilient：单次调用内重试（SATH_MODEL_RETRY_*），401/429/5xx/传输错误可重试。
//  2. 本文件 callModel：单次调用重试耗尽后，冷却（EnvModelRecoveryDelays）再以相同 messages
//     重试同一步；已执行的工具不会重跑。
//
// 两层的「等待」共用一个 Run 级 model.RetryWaitBudget（EnvModelRecoveryBudget，默认 120s），经 ctx
// 传递给 WrapResilient 与子 agent；只计退避/冷却，不计生成耗时。下一次等待超出剩余预算或 ctx
// deadline 时不再等待，直接进入降级。
//
// 降级：可恢复类模型错误最终仍失败、且本 Run 已有工具结果时，以工具结果拼出确定性的部分结果作为终答
// （ModelUnavailableAnswer，开头为 ModelUnavailableAnswerPrefix）：
//   - 非流式 Run 返回 error=nil，Response.Metadata["model_unavailable"]=true，且 trace.ModelUnavailable=true、
//     trace.Errors 含该模型错误；
//   - 流式 RunEvents 以 delta + done（Metadata["model_unavailable"]=true）结束而非 error。流式界面上可能依次出现
//     半截正文、ModelRetryNotice 与重试后的完整正文，而 done.Text / 落库消息只含最终正文。
//
// 不可恢复错误（400 上下文超长、内容过滤、403、404 等）或尚无工具结果时沿用原报错路径。

// EnvModelRecoveryDelays 覆盖步骤级模型恢复的冷却序列（毫秒，逗号分隔），
// 如 "10000,20000,40000"；"0"/"off" 关闭。设置后优先于 ReActConfig.ModelRecoveryDelays
// （例外：显式 WithReActModelRecoveryDelays() 关闭恢复时以配置为准，供子 agent 使用）。
const EnvModelRecoveryDelays = "SATH_MODEL_RECOVERY_DELAYS_MS"

// EnvModelRecoveryBudget 覆盖 Run 级模型重试等待总预算（毫秒），覆盖两层等待；设置后优先于配置。
const EnvModelRecoveryBudget = "SATH_MODEL_RECOVERY_BUDGET_MS"

// DefaultModelRecoveryDelays 为默认冷却序列（3 次恢复）。
var DefaultModelRecoveryDelays = []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second}

// DefaultModelRecoveryBudget 为默认 Run 级等待预算。
const DefaultModelRecoveryBudget = 120 * time.Second

// criticRetryWaitBudget 为 critic 模型调用的等待上限：critic 失败即放行，不值得久等。
const criticRetryWaitBudget = 10 * time.Second

// ModelRetryNotice 在流式输出中途断开、即将重试本步时补发，避免两次尝试的正文黏连。
const ModelRetryNotice = "\n\n（模型连接中断，正在重试…）\n\n"

// ModelUnavailableAnswerPrefix 为降级答复的固定开头；评测/调用方可据此识别降级结果。
const ModelUnavailableAnswerPrefix = "模型服务暂时不可用（"

const (
	degradedMaxCalls    = 40
	degradedArgsRunes   = 200
	degradedResultRunes = 300
	degradedErrorRunes  = 160
)

// errStreamAborted 表示向客户端投递事件失败（已取消/断连），调用方应静默结束。
var errStreamAborted = errors.New("stream aborted")

// WithReActModelRecoveryDelays 设置步骤级模型恢复的冷却序列；零参数即关闭恢复。
func WithReActModelRecoveryDelays(delays ...time.Duration) ReActOption {
	return func(c *ReActConfig) {
		c.ModelRecoveryDelays = append([]time.Duration{}, delays...)
	}
}

// WithReActModelRecoverySleep 注入冷却等待函数（测试用）；nil 恢复默认。
func WithReActModelRecoverySleep(fn func(context.Context, time.Duration) error) ReActOption {
	return func(c *ReActConfig) {
		c.ModelRecoverySleep = fn
	}
}

// WithReActModelRecoveryBudget 设置 Run 级等待预算（ctx 上已有预算时沿用 ctx 的，如子 agent）。
func WithReActModelRecoveryBudget(d time.Duration) ReActOption {
	return func(c *ReActConfig) {
		c.ModelRecoveryBudget = &d
	}
}

func (a *ReActAgent) modelRecoveryDelays() []time.Duration {
	if a != nil && a.config.ModelRecoveryDelays != nil && len(a.config.ModelRecoveryDelays) == 0 {
		return a.config.ModelRecoveryDelays
	}
	if d, ok := modelRecoveryDelaysFromEnv(); ok {
		return d
	}
	if a != nil && a.config.ModelRecoveryDelays != nil {
		return a.config.ModelRecoveryDelays
	}
	return DefaultModelRecoveryDelays
}

func modelRecoveryBudgetTotal(configured *time.Duration) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(EnvModelRecoveryBudget))); err == nil && n >= 0 {
		return time.Duration(n) * time.Millisecond
	}
	if configured != nil {
		return *configured
	}
	return DefaultModelRecoveryBudget
}

// withRunWaitBudget 确保 ctx 带有 Run 级等待预算；已有（父 agent / plan agent 传入）则沿用，
// 使嵌套调用共享同一上限。
func withRunWaitBudget(ctx context.Context, configured *time.Duration) context.Context {
	if model.RetryWaitBudgetFrom(ctx) != nil {
		return ctx
	}
	return model.WithRetryWaitBudget(ctx, model.NewRetryWaitBudget(modelRecoveryBudgetTotal(configured)))
}

// withSubWaitBudget 为局部调用派生更小的等待上限（同时受父预算约束）。
func withSubWaitBudget(ctx context.Context, limit time.Duration) context.Context {
	if parent := model.RetryWaitBudgetFrom(ctx); parent != nil {
		return model.WithRetryWaitBudget(ctx, parent.Sub(limit))
	}
	return model.WithRetryWaitBudget(ctx, model.NewRetryWaitBudget(limit))
}

func modelRecoveryDelaysFromEnv() ([]time.Duration, bool) {
	raw := strings.TrimSpace(os.Getenv(EnvModelRecoveryDelays))
	if raw == "" {
		return nil, false
	}
	switch strings.ToLower(raw) {
	case "0", "off", "false", "none":
		return []time.Duration{}, true
	}
	var out []time.Duration
	for _, part := range strings.Split(raw, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, time.Duration(n)*time.Millisecond)
	}
	return out, true
}

func (a *ReActAgent) recoverySleep(ctx context.Context, d time.Duration) error {
	if a != nil && a.config.ModelRecoverySleep != nil {
		return a.config.ModelRecoverySleep(ctx, d)
	}
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// callModel 执行一次模型调用；遇到可恢复的模型错误（WrapResilient 已重试耗尽）时，
// 按 Run 级冷却序列与等待预算冷却后以相同输入重试。call 必须只包含模型调用本身（不得执行工具）。
// beforeRetry 在决定重试、开始冷却前调用（可为 nil），返回 false 放弃重试。
// 每次 call 内 WrapResilient 的重试次数累加到 trace.ModelCallRetries。
func (a *ReActAgent) callModel(
	ctx context.Context,
	trace *RunTrace,
	emit func(events.Kind, map[string]any),
	step int,
	beforeRetry func() bool,
	call func() error,
) error {
	budget := model.RetryWaitBudgetFrom(ctx)
	for {
		before := budget.Retries()
		err := call()
		if trace != nil {
			trace.ModelCallRetries += budget.Retries() - before
		}
		if err == nil {
			return nil
		}
		if ctx.Err() != nil || trace == nil || !model.IsRetryableModelError(err) {
			return err
		}
		delays := a.modelRecoveryDelays()
		n := trace.ModelRecoveries
		if n >= len(delays) {
			return err
		}
		delay := delays[n]
		// 等不完这次冷却（ctx 将超时或预算不足）就不等：直接返回错误，由调用方降级给出部分结果。
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay {
			return err
		}
		if budget != nil && delay > budget.Remaining() {
			return err
		}
		if beforeRetry != nil && !beforeRetry() {
			return err
		}
		if !budget.TryReserve(delay) {
			return err
		}
		trace.ModelRecoveries++
		trace.ModelRecoveryErrors = append(trace.ModelRecoveryErrors, err.Error())
		emit(events.ModelRecovering, map[string]any{
			"error":        err.Error(),
			"step":         step,
			"attempt":      n + 1,
			"max_attempts": len(delays),
			"delay_ms":     delay.Milliseconds(),
		})
		if serr := a.recoverySleep(ctx, delay); serr != nil {
			return err
		}
	}
}

// degradeOnModelError 在可恢复类模型错误最终仍失败、且本 Run 已有工具结果时生成确定性的部分结果
// （未经模型总结）并按普通终答落记忆；不可恢复错误、取消或尚无工具结果时返回 false，沿用报错路径。
func (a *ReActAgent) degradeOnModelError(ctx context.Context, trace *RunTrace, err error, emit func(events.Kind, map[string]any)) (string, bool) {
	if err == nil || errors.Is(err, errStreamAborted) || canceled(ctx) || trace == nil || len(trace.ToolCalls) == 0 {
		return "", false
	}
	if !model.IsRetryableModelError(err) {
		return "", false
	}
	text := ModelUnavailableAnswer(trace.ToolCalls, err)
	trace.ModelUnavailable = true
	trace.Errors = append(trace.Errors, err.Error())
	_ = a.storeAssistant(ctx, text)
	emit(events.RunCompleted, map[string]any{
		"text_length":       len(text),
		"tool_calls":        len(trace.ToolCalls),
		"model_unavailable": true,
		"error":             err.Error(),
	})
	return text, true
}

func (a *ReActAgent) degradedResponse(ctx context.Context, trace *RunTrace, messages []model.Message, err error, emit func(events.Kind, map[string]any)) (*Response, bool) {
	text, ok := a.degradeOnModelError(ctx, trace, err, emit)
	if !ok {
		return nil, false
	}
	resp := responseWithTrace(text, nil, trace, messages)
	resp.Metadata["model_unavailable"] = true
	return resp, true
}

// finishDegradedStream 以降级答复收尾流式 Run（delta + done）；返回 false 表示未降级，调用方应 sendError。
// separate 为 true 时先补换行，与本步已流出的半截正文分段。
func (a *ReActAgent) finishDegradedStream(
	ctx context.Context,
	trace *RunTrace,
	messages []model.Message,
	err error,
	separate bool,
	emit func(events.Kind, map[string]any),
	send func(StreamEvent) bool,
) bool {
	text, ok := a.degradeOnModelError(ctx, trace, err, emit)
	if !ok {
		return false
	}
	delta := text
	if separate {
		delta = "\n\n" + text
	}
	if send(StreamEvent{Type: StreamEventDelta, Text: delta, Trace: trace}) {
		_ = send(streamDoneEvent(trace, messages, map[string]any{"model_unavailable": true}, text))
	}
	return true
}

// ModelUnavailableAnswer 构造模型不可用时的部分结果：错误摘要 + 已执行工具调用的参数/状态/结果预览。
func ModelUnavailableAnswer(records []ToolCallRecord, err error) string {
	var b strings.Builder
	b.WriteString(ModelUnavailableAnswerPrefix)
	b.WriteString(modelErrorBrief(err))
	b.WriteString("），以下是已完成步骤的结果，未经模型总结。\n\n")
	fmt.Fprintf(&b, "已执行的查询（共 %d 次）：\n\n", len(records))
	shown := records
	if len(shown) > degradedMaxCalls {
		shown = shown[:degradedMaxCalls]
	}
	for i, r := range shown {
		fmt.Fprintf(&b, "%d. **%s** · 状态：%s\n", i+1, inlineSafe(r.ToolName), degradedCallStatus(r))
		if args := degradedArgs(r.Arguments); args != "" {
			fmt.Fprintf(&b, "   - 参数：`%s`\n", args)
		}
		if r.Error != "" {
			fmt.Fprintf(&b, "   - 错误：%s\n", oneLine(redact.String(r.Error), degradedResultRunes))
		} else if p := oneLine(previewResult(r.Result), degradedResultRunes); p != "" {
			fmt.Fprintf(&b, "   - 结果：%s\n", p)
		}
	}
	if extra := len(records) - len(shown); extra > 0 {
		fmt.Fprintf(&b, "\n（另有 %d 次调用未列出）\n", extra)
	}
	b.WriteString("\n请稍后重试，以获得完整的分析结论。")
	return b.String()
}

func modelErrorBrief(err error) string {
	if err == nil {
		return "未知错误"
	}
	msg := oneLine(redact.String(err.Error()), degradedErrorRunes)
	if code, ok := model.ModelErrorStatus(err); ok {
		return fmt.Sprintf("HTTP %d：%s", code, msg)
	}
	return msg
}

func degradedCallStatus(r ToolCallRecord) string {
	switch {
	case r.Blocked:
		return "blocked"
	case r.HitStatus != "":
		return r.HitStatus
	case r.Error != "":
		return "error"
	default:
		return "ok"
	}
}

func degradedArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	b, err := json.Marshal(redactArgs(args))
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(truncateRunes(string(b), degradedArgsRunes), "`", "'")
}

// oneLine 折叠空白并截断，保证预览不破坏 Markdown 列表结构。
func oneLine(s string, max int) string {
	return inlineSafe(truncateRunes(strings.Join(strings.Fields(s), " "), max))
}

func inlineSafe(s string) string {
	return strings.ReplaceAll(s, "`", "'")
}
