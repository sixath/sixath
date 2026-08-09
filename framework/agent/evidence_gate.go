package agent

import (
	"errors"
	"strings"

	"github.com/sixath/framework/tool"
)

// ErrEvidenceGateHalt is returned when EvidenceGate HardHalt blocks a final answer.
var ErrEvidenceGateHalt = errors.New("evidence gate halt")

// EvidenceGateConfig controls final-answer evidence checks (E1 Soft/Hard).
type EvidenceGateConfig struct {
	Enabled            bool
	HardHalt           bool
	RequireAnyOf       []string // default jaeger_trace, es_log_query if nil/empty when evaluating
	InsufficientOKText []string // default "证据不足", "insufficient evidence"
}

// EvidenceGateResult is the outcome of EvaluateEvidenceGate.
type EvidenceGateResult struct {
	Allow  bool
	Action string // "" | "inject" | "halt"
	Reason string
	Prompt string
}

var (
	defaultRequireAnyOf       = []string{"jaeger_trace", "es_log_query"}
	defaultInsufficientOKText = []string{"证据不足", "insufficient evidence"}
)

const evidenceGateSoftPrompt = `证据不足：请先用 jaeger_trace 或 es_log_query 收集运行时证据后再下结论；若确实无法取得证据，请在答复中明确写「证据不足」或 "insufficient evidence"。
Insufficient evidence: gather jaeger_trace / es_log_query evidence before concluding, or explicitly say 证据不足 / "insufficient evidence".`

// EvaluateEvidenceGate checks whether finalText is allowed given accumulated evidence refs.
func EvaluateEvidenceGate(cfg EvidenceGateConfig, refs []tool.EvidenceRef, finalText string) EvidenceGateResult {
	if !cfg.Enabled {
		return EvidenceGateResult{Allow: true}
	}

	okTexts := cfg.InsufficientOKText
	if len(okTexts) == 0 {
		okTexts = defaultInsufficientOKText
	}
	lowerFinal := strings.ToLower(finalText)
	for _, phrase := range okTexts {
		if phrase == "" {
			continue
		}
		if strings.Contains(finalText, phrase) || strings.Contains(lowerFinal, strings.ToLower(phrase)) {
			return EvidenceGateResult{Allow: true}
		}
	}

	require := cfg.RequireAnyOf
	if len(require) == 0 {
		require = defaultRequireAnyOf
	}
	allowed := make(map[string]struct{}, len(require))
	for _, k := range require {
		if k != "" {
			allowed[k] = struct{}{}
		}
	}
	for _, ref := range refs {
		if ref.Kind == "" {
			continue
		}
		if _, ok := allowed[ref.Kind]; ok {
			return EvidenceGateResult{Allow: true}
		}
	}

	reason := "missing required evidence refs (need one of: " + strings.Join(require, ", ") + ")"
	if cfg.HardHalt {
		return EvidenceGateResult{
			Allow:  false,
			Action: "halt",
			Reason: reason,
		}
	}
	return EvidenceGateResult{
		Allow:  false,
		Action: "inject",
		Reason: reason,
		Prompt: evidenceGateSoftPrompt,
	}
}
