package chat

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"unicode"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
)

// TurnIntentGate is the Portal PostModelPolicy (L2 v0):
//   - Final-answer text + tool_calls → discard tools and finish
//   - Drift-sensitive tools whose args share no tokens with the user query → drop
const (
	turnIntentGateEnv          = "SATH_TURN_INTENT_GATE"
	finalAnswerMinRunes        = 80
	topicOverlapMinTokenLen    = 2
)

// driftSensitiveTools are tools that often open a new research topic.
var driftSensitiveTools = map[string]struct{}{
	"web_search":       {},
	"web_extract":      {},
	"knowledge_search": {},
	"knowledge_read":   {},
	"memory_search":    {},
	"session_search":   {},
}

var finalAnswerCues = []string{
	"请告诉我",
	"如有需要",
	"如果还需要",
	"有其他问题",
	"还有什么问题",
	"希望对你有帮助",
	"希望以上",
	"分析完成",
	"总结完毕",
	"以上就是",
	"let me know",
	"feel free to ask",
	"hope this helps",
	"if you need anything else",
}

// TurnIntentGate implements agent.PostModelPolicy.
type TurnIntentGate struct {
	ActiveFamilies map[string]struct{} // nil => skip family filter
	ToolFamily     map[string]string   // tool name → family id
}

// NewTurnIntentGate returns the default gate, or nil when disabled via env.
func NewTurnIntentGate() agent.PostModelPolicy {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(turnIntentGateEnv)))
	if v == "0" || v == "false" || v == "off" || v == "no" {
		return nil
	}
	return TurnIntentGate{}
}

// NewTurnIntentGateWithSurface returns a gate with per-turn family filter, or nil when disabled via env.
func NewTurnIntentGateWithSurface(active map[string]struct{}, toolFamily map[string]string) agent.PostModelPolicy {
	if NewTurnIntentGate() == nil {
		return nil
	}
	return TurnIntentGate{ActiveFamilies: active, ToolFamily: toolFamily}
}

// TurnIntentGateOption returns a ReActOption that installs a surface-aware gate.
// When the gate is disabled via env, returns a no-op so callers can always append it.
// Pass as BuildReActAgent extra so it overrides the default NewTurnIntentGate() (last option wins).
func TurnIntentGateOption(active map[string]struct{}, toolFamily map[string]string) agent.ReActOption {
	gate := NewTurnIntentGateWithSurface(active, toolFamily)
	if gate == nil {
		return func(*agent.ReActConfig) {}
	}
	return agent.WithReActPostModelPolicy(gate)
}

// Evaluate implements agent.PostModelPolicy.
func (g TurnIntentGate) Evaluate(_ context.Context, in agent.PostModelPolicyInput) agent.PostModelPolicyResult {
	calls := toolCallsFromModelStep(in.ToolStep)
	if len(calls) == 0 {
		return agent.PostModelPolicyResult{Decision: agent.PostModelContinue}
	}
	if looksLikeFinalAnswer(in.AssistantText) {
		return agent.PostModelPolicyResult{
			Decision: agent.PostModelFinish,
			Reason:   "final_answer_discard_tools",
		}
	}
	calls, familyDropped := filterCallsByFamily(calls, g.ActiveFamilies, g.ToolFamily)
	if len(calls) == 0 {
		return agent.PostModelPolicyResult{
			Decision: agent.PostModelFinish,
			Reason:   "family_not_active",
		}
	}
	user := lastUserContent(in.Req)
	userToks := tokenizeForOverlap(user)
	if len(userToks) == 0 {
		if familyDropped > 0 {
			return agent.PostModelPolicyResult{
				Decision:  agent.PostModelFilter,
				ToolCalls: calls,
				Reason:    "family_partial",
			}
		}
		return agent.PostModelPolicyResult{Decision: agent.PostModelContinue}
	}
	kept := make([]model.ToolCall, 0, len(calls))
	driftDropped := 0
	for _, c := range calls {
		if !isDriftSensitive(c.Name) {
			kept = append(kept, c)
			continue
		}
		if toolOverlapsUser(c, userToks) {
			kept = append(kept, c)
			continue
		}
		driftDropped++
	}
	if driftDropped == 0 {
		if familyDropped > 0 {
			return agent.PostModelPolicyResult{
				Decision:  agent.PostModelFilter,
				ToolCalls: calls,
				Reason:    "family_partial",
			}
		}
		return agent.PostModelPolicyResult{Decision: agent.PostModelContinue}
	}
	if len(kept) == 0 {
		return agent.PostModelPolicyResult{
			Decision: agent.PostModelFinish,
			Reason:   "topic_drift_all",
		}
	}
	return agent.PostModelPolicyResult{
		Decision:  agent.PostModelFilter,
		ToolCalls: kept,
		Reason:    "topic_drift_partial",
	}
}

func filterCallsByFamily(calls []model.ToolCall, active map[string]struct{}, toolFamily map[string]string) ([]model.ToolCall, int) {
	if active == nil {
		return calls, 0
	}
	kept := make([]model.ToolCall, 0, len(calls))
	dropped := 0
	for _, c := range calls {
		fam := FamilyForBuiltinToolName(c.Name)
		if toolFamily != nil {
			if f, ok := toolFamily[c.Name]; ok {
				fam = f
			}
		}
		if FamilyActive(active, fam) {
			kept = append(kept, c)
		} else {
			dropped++
		}
	}
	return kept, dropped
}

func toolCallsFromModelStep(step model.ToolStep) []model.ToolCall {
	if len(step.ToolCalls) > 0 {
		return step.ToolCalls
	}
	if !step.Used {
		return nil
	}
	return []model.ToolCall{{
		ID:        step.ToolCallID,
		Name:      step.ToolName,
		Arguments: step.Arguments,
	}}
}

func looksLikeFinalAnswer(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if utf8RuneCount(text) < finalAnswerMinRunes {
		return false
	}
	lower := strings.ToLower(text)
	// Prefer cues near the end (last ~400 runes).
	tail := text
	runes := []rune(text)
	if len(runes) > 400 {
		tail = string(runes[len(runes)-400:])
		lower = strings.ToLower(tail)
	}
	for _, cue := range finalAnswerCues {
		if cue == "" {
			continue
		}
		if strings.Contains(tail, cue) || strings.Contains(lower, strings.ToLower(cue)) {
			return true
		}
	}
	return false
}

func lastUserContent(req *agent.Request) string {
	if req == nil {
		return ""
	}
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			return strings.TrimSpace(req.Messages[i].Content)
		}
	}
	return ""
}

func isDriftSensitive(name string) bool {
	_, ok := driftSensitiveTools[strings.TrimSpace(name)]
	return ok
}

func toolOverlapsUser(call model.ToolCall, userToks map[string]struct{}) bool {
	blob := call.Name
	if call.Arguments != nil {
		if b, err := json.Marshal(call.Arguments); err == nil {
			blob += " " + string(b)
		} else {
			for _, v := range call.Arguments {
				blob += " " + stringifyArg(v)
			}
		}
	}
	toolToks := tokenizeForOverlap(blob)
	for t := range toolToks {
		if _, ok := userToks[t]; ok {
			return true
		}
	}
	return false
}

func stringifyArg(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func tokenizeForOverlap(s string) map[string]struct{} {
	out := map[string]struct{}{}
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return out
	}
	var latin strings.Builder
	flushLatin := func() {
		w := latin.String()
		latin.Reset()
		if len(w) >= topicOverlapMinTokenLen {
			out[w] = struct{}{}
		}
	}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if unicode.Is(unicode.Han, r) {
			flushLatin()
			// CJK bigrams
			if i+1 < len(runes) && unicode.Is(unicode.Han, runes[i+1]) {
				out[string(runes[i:i+2])] = struct{}{}
			}
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			latin.WriteRune(r)
			continue
		}
		flushLatin()
	}
	flushLatin()
	return out
}

func utf8RuneCount(s string) int {
	return len([]rune(s))
}
