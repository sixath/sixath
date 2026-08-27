package agent

import (
	"os"
	"strings"
	"unicode"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/tool"
)

const (
	emptyHitSpeakReason = "empty_hit_speak"
	emptyHitGateEnv     = "SATH_EMPTY_HIT_GATE"
)

var emptyHitDenyA = []string{
	"从未参与",
	"从未出现",
	"服务不存在",
	"没有这个服务",
	"never participated",
	"service does not exist",
}

var emptyHitDenyB = []string{
	"不存在",
	"没有参与",
	"从未调用",
	"does not exist",
}

var emptyHitAllowPhrases = []string{
	"不能据此说从未参与",
	"不能说从未参与",
	"cannot conclude never",
	"未查到",
	"0 条",
	"0 hits",
	"没有匹配行",
}

func emptyHitSpeakGateDisabled() bool {
	return strings.TrimSpace(os.Getenv(emptyHitGateEnv)) == "0"
}

func EvaluateEmptyHitSpeakGate(trace *RunTrace, finalText string) EvidenceGateResult {
	if emptyHitSpeakGateDisabled() {
		return EvidenceGateResult{Allow: true}
	}
	scopes, hasEmpty := collectEmptySpeakScopes(trace)
	if !hasEmpty {
		return EvidenceGateResult{Allow: true}
	}
	norm := normalizeEmptyHitText(finalText)
	work := stripEmptyHitAllowPhrases(norm)
	if containsAnySubstr(work, emptyHitDenyA) {
		return emptyHitReject(scopes)
	}
	scoped := textHasAnyScope(norm, scopes) // 范围看删除豁免前的原文，index 才还在
	skipBExist := redisKeyAbsence(norm)
	if !scoped {
		for _, p := range emptyHitDenyB {
			if skipBExist && (p == "不存在" || p == "does not exist") {
				continue
			}
			if strings.Contains(work, p) {
				return emptyHitReject(scopes)
			}
		}
	}
	return EvidenceGateResult{Allow: true}
}

func emptyHitReject(scopes []string) EvidenceGateResult {
	listed := strings.Join(scopes, ", ")
	if listed == "" {
		listed = "es_log_query/execute_read"
	}
	prompt := "空击（0 条）只能写未查到，不能写从未参与 / 服务不存在。本轮查询范围：" + listed +
		"。换 index 再查是建议，不是必须。弱命题须带上上述 index/repo。"
	return EvidenceGateResult{Allow: false, Action: "inject", Reason: emptyHitSpeakReason, Prompt: prompt}
}

func collectEmptySpeakScopes(trace *RunTrace) (scopes []string, hasEmpty bool) {
	if trace == nil {
		return nil, false
	}
	for _, rec := range trace.ToolCalls {
		if rec.Error != "" || rec.Blocked {
			continue
		}
		switch rec.ToolName {
		case "es_log_query", "execute_read":
		default:
			continue
		}
		st, idx, repo := tool.HitContractFromResult(rec.Result)
		if st != tool.HitStatusEmpty {
			continue
		}
		hasEmpty = true
		if idx != "" {
			scopes = append(scopes, idx)
		}
		if repo != "" {
			scopes = append(scopes, repo)
		}
	}
	return scopes, hasEmpty
}

func normalizeEmptyHitText(s string) string {
	s = strings.ToLower(s)
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

func stripEmptyHitAllowPhrases(s string) string {
	for _, p := range emptyHitAllowPhrases {
		s = strings.ReplaceAll(s, p, "")
	}
	return s
}

func textHasAnyScope(norm string, scopes []string) bool {
	for _, sc := range scopes {
		if sc != "" && strings.Contains(norm, sc) {
			return true
		}
	}
	return false
}

func redisKeyAbsence(norm string) bool {
	if !strings.Contains(norm, "redis") {
		return false
	}
	if !strings.Contains(norm, "key") && !strings.Contains(norm, "键") {
		return false
	}
	return strings.Contains(norm, "不存在") || strings.Contains(norm, "nil")
}

func containsAnySubstr(s string, phrases []string) bool {
	for _, p := range phrases {
		if p != "" && strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func (a *ReActAgent) checkEmptyHitSpeakGate(trace *RunTrace, finalText string, allowInject, hasStepRoom bool, emit func(events.Kind, map[string]any)) evidenceGateCheck {
	result := EvaluateEmptyHitSpeakGate(trace, finalText)
	if result.Allow {
		return evidenceGateCheck{}
	}
	if allowInject && result.Action == "inject" && trace != nil && trace.EmptyHitNudges == 0 && hasStepRoom {
		return evidenceGateCheck{Inject: true, EmptyHit: true, Prompt: result.Prompt}
	}
	if trace != nil {
		trace.Errors = append(trace.Errors, emptyHitSpeakReason)
	}
	if emit != nil {
		emit(events.EvidenceIncomplete, map[string]any{"reason": emptyHitSpeakReason})
	}
	return evidenceGateCheck{Incomplete: true, EmptyHit: true}
}
