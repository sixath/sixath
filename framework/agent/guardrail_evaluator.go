package agent

import "github.com/sixath/framework/events"

// GuardrailDecision 为单次护栏评估结果（设计 §6.2）；Warn 表示本回合至少发出过一次 ToolGuardrailWarn。
type GuardrailDecision struct {
	Warn bool
	Halt bool
}

// GuardrailEvaluator 在 executeToolStep 与 appendToolResultMessages 之后评估护栏（设计 §6.2）。
// 实现须通过 emit 发布 ToolGuardrailWarn；Halt 为 true 时由 ReAct 终止本 Run 并写 trace。
type GuardrailEvaluator interface {
	Evaluate(history []ToolCallRecord, consecutiveToolOnlyModelRounds int, emit func(events.Kind, map[string]any)) GuardrailDecision
}

type noopGuardrailEvaluator struct{}

func (noopGuardrailEvaluator) Evaluate([]ToolCallRecord, int, func(events.Kind, map[string]any)) GuardrailDecision {
	return GuardrailDecision{}
}

// defaultGuardrailEvaluator 将设计 R1/R2/R3 规则委托给 applyToolGuardrails（与既有 YAML / trace 语义一致）。
type defaultGuardrailEvaluator struct {
	cfg *ToolGuardrailsConfig
}

// NewGuardrailEvaluator 由 ToolGuardrailsConfig 构造默认评估器；cfg 为 nil 或未启用时返回 noop。
func NewGuardrailEvaluator(cfg *ToolGuardrailsConfig) GuardrailEvaluator {
	if cfg == nil || !cfg.Enabled {
		return noopGuardrailEvaluator{}
	}
	cp := *cfg
	if cfg.IdempotentTools != nil {
		cp.IdempotentTools = append([]string(nil), cfg.IdempotentTools...)
	}
	if cfg.MutatingTools != nil {
		cp.MutatingTools = append([]string(nil), cfg.MutatingTools...)
	}
	return &defaultGuardrailEvaluator{cfg: &cp}
}

func (e *defaultGuardrailEvaluator) Evaluate(history []ToolCallRecord, consecutiveToolOnly int, emit func(events.Kind, map[string]any)) GuardrailDecision {
	if e == nil || e.cfg == nil {
		return GuardrailDecision{}
	}
	var warned bool
	wrap := func(k events.Kind, p map[string]any) {
		if k == events.ToolGuardrailWarn {
			warned = true
		}
		if emit != nil {
			emit(k, p)
		}
	}
	halt := applyToolGuardrails(e.cfg, history, wrap, consecutiveToolOnly)
	return GuardrailDecision{Warn: warned, Halt: halt}
}
