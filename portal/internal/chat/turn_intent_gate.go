package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
)

// TurnIntentGate is the Portal PostModelPolicy (L2 v0):
//   - Final-answer text + tool_calls → discard tools and finish
//   - Drift-sensitive tools whose args share no tokens with the user query → drop
const (
	turnIntentGateEnv       = "SATH_TURN_INTENT_GATE"
	finalAnswerMinRunes     = 80
	topicOverlapMinTokenLen = 2
)

// driftSensitiveTools are tools that often open a new research topic.
// load_skill is omitted: args are only the skill name, so Q-overlap false-positives
// Finish the turn. skill_view / execute_skill_script stay listed, but a skill already
// opened this turn via load_skill/skill_view is treated as on-topic.
var driftSensitiveTools = map[string]struct{}{
	"web_search":           {},
	"web_extract":          {},
	"knowledge_search":     {},
	"knowledge_read":       {},
	"memory_search":        {},
	"session_search":       {},
	"skill_view":           {},
	"execute_skill_script": {},
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

var (
	_ agent.PostModelPolicy     = TurnIntentGate{}
	_ agent.IdlePostModelPolicy = TurnIntentGate{}
)

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
	lock := TaskLockFromRequest(in.Req)
	q := overlapQuery(in.Req, lock)
	orig := calls
	calls, familyDropped := filterCallsByFamily(calls, g.ActiveFamilies, g.ToolFamily)
	recordDroppedBetween(in.Trace, orig, calls)
	if len(calls) == 0 {
		return agent.PostModelPolicyResult{
			Decision: agent.PostModelRetry,
			Reason:   "family_dropped_all",
			Prompt:   familyRetryPrompt(g.ActiveFamilies),
		}
	}
	keptAfterIntake := make([]model.ToolCall, 0, len(calls))
	intakeDropped := 0
	for _, c := range calls {
		if strings.TrimSpace(c.Name) == "ask_user" && isIntakeAskUser(c.Arguments) && blockIntakeAskUser(lock, q) {
			intakeDropped++
			recordDroppedSkill(in.Trace, c)
			continue
		}
		keptAfterIntake = append(keptAfterIntake, c)
	}
	if intakeDropped > 0 && len(keptAfterIntake) == 0 {
		return agent.PostModelPolicyResult{
			Decision: agent.PostModelRetry,
			Reason:   "intake_ask_user",
			Prompt:   intakeAskUserRetryPrompt(lock),
		}
	}
	calls = keptAfterIntake
	userToks := tokenizeForOverlap(q)
	if len(userToks) == 0 {
		if familyDropped > 0 || intakeDropped > 0 {
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
		if toolOverlapsUser(c, userToks) || skillCallOverlapsQ(c, q) || skillOpenedThisTurn(in.Trace, skillArgName(c)) {
			kept = append(kept, c)
			continue
		}
		driftDropped++
		if isSkillTool(c.Name) {
			recordDroppedSkill(in.Trace, c)
		}
	}
	if driftDropped == 0 {
		if familyDropped > 0 || intakeDropped > 0 {
			reason := "family_partial"
			if familyDropped == 0 {
				reason = "intake_partial"
			}
			return agent.PostModelPolicyResult{
				Decision:  agent.PostModelFilter,
				ToolCalls: calls,
				Reason:    reason,
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

// EvaluateIdle implements agent.IdlePostModelPolicy.
func (g TurnIntentGate) EvaluateIdle(_ context.Context, in agent.PostModelPolicyInput) agent.PostModelPolicyResult {
	lock := TaskLockFromRequest(in.Req)
	if looksLikeGoalDrift(lock, in.AssistantText, in.Trace) && in.Trace != nil && in.Trace.GoalDriftNudges == 0 {
		in.Trace.GoalDriftNudges++
		return agent.PostModelPolicyResult{
			Decision: agent.PostModelRetry,
			Reason:   "goal_drift",
			Prompt:   goalDriftRetryPrompt(lock),
		}
	}
	return agent.PostModelPolicyResult{Decision: agent.PostModelContinue}
}

func filterCallsByFamily(calls []model.ToolCall, active map[string]struct{}, toolFamily map[string]string) ([]model.ToolCall, int) {
	if active == nil {
		return calls, 0
	}
	kept := make([]model.ToolCall, 0, len(calls))
	dropped := 0
	for _, c := range calls {
		fam := callFamily(c.Name, toolFamily)
		if FamilyActive(active, fam) {
			kept = append(kept, c)
		} else {
			dropped++
		}
	}
	return kept, dropped
}

func callFamily(name string, toolFamily map[string]string) string {
	if toolFamily != nil {
		if f, ok := toolFamily[name]; ok && f != "" {
			return f
		}
	}
	return FamilyForBuiltinToolName(name)
}

func familyRetryPrompt(active map[string]struct{}) string {
	names := make([]string, 0, len(active))
	for id := range active {
		if strings.TrimSpace(id) != "" {
			names = append(names, id)
		}
	}
	sort.Strings(names)
	list := strings.Join(names, ", ")
	if list == "" {
		list = FamilyCore
	}
	return fmt.Sprintf("本轮激活的工具族是 %s。刚才那些调用不属于这些族，不要执行。请只用这些族内的工具继续，或直接作答。", list)
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

func overlapQuery(req *agent.Request, lock *TurnTaskLock) string {
	if lock != nil && strings.TrimSpace(lock.Q) != "" {
		return strings.TrimSpace(lock.Q)
	}
	return lastUserContent(req)
}

func isSkillTool(name string) bool {
	return agent.IsSkillsFamilyToolName(strings.TrimSpace(name))
}

func skillArgName(call model.ToolCall) string {
	if call.Arguments == nil {
		return ""
	}
	if n, ok := call.Arguments["name"].(string); ok {
		return strings.TrimSpace(n)
	}
	return ""
}

func recordDroppedSkill(trace *agent.RunTrace, call model.ToolCall) {
	if trace == nil {
		return
	}
	trace.DroppedProposals = append(trace.DroppedProposals, agent.DroppedProposal{
		ToolName: call.Name,
		ArgName:  skillArgName(call),
	})
}

func recordDroppedBetween(trace *agent.RunTrace, orig, kept []model.ToolCall) {
	if trace == nil {
		return
	}
	keptIDs := make(map[string]struct{}, len(kept))
	for _, c := range kept {
		keptIDs[callKey(c)] = struct{}{}
	}
	for _, c := range orig {
		if _, ok := keptIDs[callKey(c)]; ok {
			continue
		}
		if isSkillTool(c.Name) {
			recordDroppedSkill(trace, c)
		}
	}
}

func callKey(c model.ToolCall) string {
	if strings.TrimSpace(c.ID) != "" {
		return c.ID
	}
	return c.Name + ":" + skillArgName(c)
}

func isIntakeAskUser(args map[string]any) bool {
	blob := stringifyArg(args)
	if args != nil {
		if p, ok := args["prompt"].(string); ok {
			blob = p + " " + blob
		}
	}
	for _, p := range []string{"请提供", "请给出", "麻烦提供", "至少一项", "必填"} {
		if strings.Contains(blob, p) {
			return true
		}
	}
	return false
}

func blockIntakeAskUser(lock *TurnTaskLock, q string) bool {
	if lock == nil {
		return false
	}
	if qLooksLikeIntake(q) {
		return false
	}
	return len(lock.KnownValues) > 0 || lock.HasPriorAssistant
}

func skillNameOverlapsQ(name, q string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	q = strings.ToLower(strings.TrimSpace(q))
	if name == "" || q == "" {
		return false
	}
	if strings.Contains(q, name) || strings.Contains(q, strings.ReplaceAll(name, "-", " ")) {
		return true
	}
	for _, part := range strings.Split(name, "-") {
		part = strings.TrimSpace(part)
		if len(part) >= 4 && strings.Contains(q, part) {
			return true
		}
	}
	return false
}

func skillCallOverlapsQ(call model.ToolCall, q string) bool {
	if !isSkillTool(call.Name) {
		return false
	}
	return skillNameOverlapsQ(skillArgName(call), q)
}

func skillOpenedThisTurn(trace *agent.RunTrace, name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || trace == nil {
		return false
	}
	for _, tc := range trace.ToolCalls {
		switch strings.TrimSpace(tc.ToolName) {
		case "load_skill", "skill_view":
		default:
			continue
		}
		if strings.TrimSpace(tc.Error) != "" {
			continue
		}
		got := ""
		if tc.Arguments != nil {
			if v, ok := tc.Arguments["name"].(string); ok {
				got = strings.ToLower(strings.TrimSpace(v))
			}
		}
		if got == name {
			return true
		}
	}
	return false
}

func looksLikeGoalDrift(lock *TurnTaskLock, assistantText string, trace *agent.RunTrace) bool {
	q := ""
	if lock != nil {
		q = lock.Q
	}
	b1, b2 := false, false
	if trace != nil {
		for _, d := range trace.DroppedProposals {
			if isSkillTool(d.ToolName) && !skillNameOverlapsQ(d.ArgName, q) {
				b1 = true
			}
			if strings.TrimSpace(d.ToolName) == "ask_user" && blockIntakeAskUser(lock, q) {
				b2 = true
			}
		}
		for _, tc := range trace.ToolCalls {
			if strings.TrimSpace(tc.ToolName) != "ask_user" {
				continue
			}
			if isIntakeAskUser(tc.Arguments) && blockIntakeAskUser(lock, q) {
				b2 = true
			}
		}
	}
	_ = assistantText // B3 文风不得单独开火；B1/B2/B4 已足够。
	return b1 || b2 || idleCatalogInsteadOfAnswer(lock, trace)
}

// idleCatalogInsteadOfAnswer is B4: follow-up turn answered with a skills catalog
// instead of G. Empty ToolCalls is false so T6 still fires via B1.
func idleCatalogInsteadOfAnswer(lock *TurnTaskLock, trace *agent.RunTrace) bool {
	if lock == nil || trace == nil {
		return false
	}
	if !lock.HasPriorAssistant {
		return false
	}
	g := strings.ToLower(lock.Q)
	for _, k := range []string{"技能", "手册", "skills_list", "load_skill", "skill_view"} {
		if strings.Contains(g, strings.ToLower(k)) {
			return false
		}
	}
	if len(trace.ToolCalls) == 0 {
		return false
	}
	for _, tc := range trace.ToolCalls {
		if !agent.IsSkillsFamilyToolName(strings.TrimSpace(tc.ToolName)) {
			return false
		}
	}
	return !agent.HasSuccessfulBoundEvidence(trace)
}

func intakeAskUserRetryPrompt(lock *TurnTaskLock) string {
	var b strings.Builder
	if lock != nil {
		b.WriteString(lock.Format())
		b.WriteString("\n")
	}
	b.WriteString("不要重新收集。用任务锁与上下文已有取值直接回答用户问题，不要调用 ask_user 再要一次。")
	return b.String()
}

func goalDriftRetryPrompt(lock *TurnTaskLock) string {
	q := ""
	if lock != nil {
		q = lock.Q
	}
	var b strings.Builder
	if lock != nil {
		b.WriteString(lock.Format())
		b.WriteString("\n")
	}
	b.WriteString("不要调用工具。用已有工具结果直接回答任务锁中的用户问题；0 击就写未查到；禁止换成另一套排查的 intake。\n")
	b.WriteString(agent.AnswerOriginalQuestionPrompt(q))
	return b.String()
}
