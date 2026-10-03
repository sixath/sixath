package harness

import (
	"context"
	"strings"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/model"
)

// DefaultMaxStopNudges 为单次 Run 内 StopHook 最多让模型继续的次数。
const DefaultMaxStopNudges = 2

// StopHookInput 是模型准备结束本轮（本步未调用任何工具）时交给 StopHook 的上下文。
type StopHookInput struct {
	// Text 为模型本步的回复正文。
	Text string
	// ToolCalls 为本 Run 已执行的工具调用。
	ToolCalls []ToolCallRecord
	// Nudges 为本 Run 已因 StopHook 继续的次数。
	Nudges int
	// HasTool 报告某工具是否注册在当前 agent；nil 时视为未知（按不可用处理）。
	HasTool func(name string) bool
	// Messages 为本步之前的对话（含用户问题），供需要上下文的 hook（如结案审查）使用。
	Messages []model.Message
	// Continues 为本 Run 已发生的继续对应的 RuleID（按顺序）。
	Continues []string
}

// StopDecision 为 StopHook 的判定结果；Continue=true 时 Message 作为下一轮输入交给模型。
type StopDecision struct {
	Continue bool
	RuleID   string
	Message  string
	// Event 非 nil 时由骨架发布到事件总线（无论是否 Continue），用于上报审查结论等。
	Event *StopHookEvent
}

// StopHookEvent 是 StopHook 请求发布的事件。
type StopHookEvent struct {
	Kind    events.Kind
	Payload map[string]any
}

// BudgetedStopHook 拥有独立的继续次数预算：RuleID 以 prefix 开头的继续只计入它自己的 max，
// 不占用 max_stop_nudges（结案审查不应被声明式规则耗尽预算，反之亦然）。
type BudgetedStopHook interface {
	StopHook
	StopBudget() (prefix string, max int)
}

// StopHook 在模型准备结束本轮时被调用，可要求模型带着 Message 继续。
// 骨架只负责调用与次数上限；判定规则由装配方提供（如 workspace/harness/hooks.yaml 的 stop_rules）。
type StopHook interface {
	OnStop(ctx context.Context, in StopHookInput) StopDecision
}

// WithReActStopHooks 注册 StopHook；零参数等价于未设置。
func WithReActStopHooks(hooks ...StopHook) ReActOption {
	return func(c *ReActConfig) {
		c.StopHooks = hooks
	}
}

// WithReActMaxStopNudges 设置 StopHook 继续次数上限；n<=0 使用 DefaultMaxStopNudges。
func WithReActMaxStopNudges(n int) ReActOption {
	return func(c *ReActConfig) {
		c.MaxStopNudges = n
	}
}

func (a *ReActAgent) maxStopNudges() int {
	if a.config.MaxStopNudges > 0 {
		return a.config.MaxStopNudges
	}
	return DefaultMaxStopNudges
}

// evaluateStopHooks 依次询问 StopHook，返回第一个要求继续的判定；hook 返回的事件经 emit 发布。
func (a *ReActAgent) evaluateStopHooks(ctx context.Context, text string, messages []model.Message, trace *RunTrace, emit func(events.Kind, map[string]any)) (StopDecision, bool) {
	if len(a.config.StopHooks) == 0 || trace == nil || ctx.Err() != nil {
		return StopDecision{}, false
	}
	var prefixes []string
	for _, h := range a.config.StopHooks {
		if b, ok := h.(BudgetedStopHook); ok {
			p, _ := b.StopBudget()
			prefixes = append(prefixes, p)
		}
	}
	shared := 0
	for _, id := range trace.StopHookContinues {
		if !hasAnyPrefix(id, prefixes) {
			shared++
		}
	}
	in := StopHookInput{Text: text, ToolCalls: trace.ToolCalls, Nudges: len(trace.StopHookContinues), Messages: messages, Continues: append([]string(nil), trace.StopHookContinues...), HasTool: func(name string) bool {
		if a.tools == nil {
			return false
		}
		_, ok := a.tools.Get(name)
		return ok
	}}
	for _, h := range a.config.StopHooks {
		if h == nil {
			continue
		}
		if b, ok := h.(BudgetedStopHook); ok {
			p, max := b.StopBudget()
			if countPrefix(trace.StopHookContinues, p) >= max {
				continue
			}
		} else if shared >= a.maxStopNudges() {
			continue
		}
		d := h.OnStop(ctx, in)
		if d.Event != nil && emit != nil {
			emit(d.Event.Kind, d.Event.Payload)
		}
		if d.Continue && strings.TrimSpace(d.Message) != "" {
			return d, true
		}
	}
	return StopDecision{}, false
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if ruleInBudget(s, p) {
			return true
		}
	}
	return false
}

func countPrefix(ids []string, prefix string) int {
	n := 0
	for _, id := range ids {
		if ruleInBudget(id, prefix) {
			n++
		}
	}
	return n
}

// ruleInBudget 报告 RuleID 是否属于预算前缀 prefix：等于 prefix，或为 prefix 加 ":"/"#" 分隔的子 ID（如 "critic:model"）。
// 仅以 prefix 开头的其他 ID（如工作区规则 "suspect_evidence_extra"）不计入。
func ruleInBudget(id, prefix string) bool {
	if prefix == "" || !strings.HasPrefix(id, prefix) {
		return false
	}
	if len(id) == len(prefix) {
		return true
	}
	switch id[len(prefix)] {
	case ':', '#':
		return true
	}
	return false
}

// continueAfterStopHook 把本步回复与 StopHook 消息追加到 messages，并记录 trace / 事件。
func continueAfterStopHook(
	messages []model.Message,
	text string,
	stepInfo model.ToolStep,
	d StopDecision,
	trace *RunTrace,
	emit func(events.Kind, map[string]any),
	step int,
) []model.Message {
	if strings.TrimSpace(text) != "" {
		messages = append(messages, assistantHistoryMessage(text, stepInfo))
	}
	messages = append(messages, model.Message{
		Role:    "user",
		Content: d.Message,
		Metadata: map[string]any{
			model.MetadataKeySixathOrigin: model.OriginStopHook,
		},
	})
	trace.StopHookContinues = append(trace.StopHookContinues, d.RuleID)
	emit(events.StopHookContinue, map[string]any{
		"rule":   d.RuleID,
		"step":   step,
		"nudges": len(trace.StopHookContinues),
	})
	return messages
}
