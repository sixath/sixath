package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sixath/framework/model"
)

// CodeClaimGateConfig controls the source-claim cascade (machine quote + LLM).
type CodeClaimGateConfig struct {
	Enabled bool
	Auditor model.Model // optional; nil uses the ReAct agent model
	Timeout time.Duration
}

const (
	defaultCodeClaimAuditorTimeout = 15 * time.Second
	codeClaimSourceBudget          = 24000
	codeClaimAuditorSystemPrompt   = `You are a read-only code-claim auditor. Compare the FINAL ANSWER against SOURCE excerpts from rca_read/rca_grep.
Task: list every side effect the answer asserts (DB write, function call, error/return code) and check whether SOURCE makes it reachable under the scenario in the user question.
If a call sits inside if/else/return in SOURCE and the answer claims it always happens (or quotes it without the guard), verdict=fail.
Reply with JSON only (no markdown), shape:
{"verdict":"pass"|"fail","issues":[{"kind":"dropped_guard|reconstructed_quote|unsupported_side_effect","path":"...","symbol":"...","guard":"...","claim":"..."}]}
Do not rewrite the analysis. Do not invent files. If SOURCE is insufficient, pass.`
)

type codeClaimPayload struct {
	Verdict string           `json:"verdict"`
	Issues  []codeClaimIssue `json:"issues"`
}

type codeClaimIssue struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Symbol string `json:"symbol"`
	Guard  string `json:"guard"`
	Claim  string `json:"claim"`
}

// EvaluateCodeClaimCascade runs machine quote check first (veto); LLM never overrides a machine fail.
func EvaluateCodeClaimCascade(ctx context.Context, auditor model.Model, userQuestion, finalText string, sources []CodeQuoteSource) EvidenceGateResult {
	if len(sources) == 0 {
		return EvidenceGateResult{Allow: true}
	}
	machine := EvaluateCodeQuoteGate(sources, finalText)
	if !machine.Allow {
		return machine
	}
	return EvaluateCodeClaimAuditor(ctx, auditor, userQuestion, finalText, sources)
}

// EvaluateCodeClaimAuditor asks a fresh-context model to judge side-effect claims.
// Nil model, chat error, or invalid JSON → fail-open (allow).
func EvaluateCodeClaimAuditor(ctx context.Context, auditor model.Model, userQuestion, finalText string, sources []CodeQuoteSource) EvidenceGateResult {
	if auditor == nil {
		return EvidenceGateResult{Allow: true}
	}
	select {
	case <-ctx.Done():
		return EvidenceGateResult{Allow: true, Reason: "code claim auditor canceled"}
	default:
	}
	gen, err := auditor.Chat(ctx, []model.Message{
		{Role: "system", Content: codeClaimAuditorSystemPrompt},
		{Role: "user", Content: buildCodeClaimUserPrompt(userQuestion, finalText, sources)},
	})
	if err != nil {
		return EvidenceGateResult{Allow: true, Reason: "code claim auditor error: " + err.Error()}
	}
	text := ""
	if gen != nil {
		text = gen.Text
	}
	parsed, ok := parseCodeClaimJSON(text)
	if !ok {
		return EvidenceGateResult{Allow: true, Reason: "code claim auditor parse failed"}
	}
	verdict := strings.ToLower(strings.TrimSpace(parsed.Verdict))
	if verdict != "fail" {
		return EvidenceGateResult{Allow: true}
	}
	if len(parsed.Issues) == 0 {
		return EvidenceGateResult{
			Allow:  false,
			Action: "inject",
			Reason: "code claim mismatch",
			Prompt: codeClaimFailPrompt(nil),
		}
	}
	return EvidenceGateResult{
		Allow:  false,
		Action: "inject",
		Reason: "code claim mismatch",
		Prompt: codeClaimFailPrompt(parsed.Issues),
	}
}

func buildCodeClaimUserPrompt(userQuestion, finalText string, sources []CodeQuoteSource) string {
	var b strings.Builder
	b.WriteString("USER QUESTION:\n")
	b.WriteString(strings.TrimSpace(userQuestion))
	b.WriteString("\n\nFINAL ANSWER:\n")
	b.WriteString(strings.TrimSpace(finalText))
	b.WriteString("\n\nSOURCE (rca_read / rca_grep excerpts):\n")
	b.WriteString(truncateCodeClaimSources(sources, codeClaimSourceBudget))
	b.WriteString("\n")
	return b.String()
}

func truncateCodeClaimSources(sources []CodeQuoteSource, budget int) string {
	if budget <= 0 {
		return ""
	}
	var b strings.Builder
	for _, src := range sources {
		header := "--- " + src.Path + " ---\n"
		remain := budget - utf8.RuneCountInString(b.String())
		if remain <= 0 {
			break
		}
		chunk := header + src.Content
		if utf8.RuneCountInString(chunk) > remain {
			runes := []rune(chunk)
			if remain < 16 {
				break
			}
			chunk = string(runes[:remain]) + "\n...[truncated]\n"
		}
		b.WriteString(chunk)
		if !strings.HasSuffix(chunk, "\n") {
			b.WriteByte('\n')
		}
	}
	if b.Len() == 0 {
		return "(none)"
	}
	return b.String()
}

func parseCodeClaimJSON(text string) (codeClaimPayload, bool) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return codeClaimPayload{}, false
	}
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```JSON")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSpace(raw)
		if i := strings.LastIndex(raw, "```"); i >= 0 {
			raw = strings.TrimSpace(raw[:i])
		}
	}
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var p codeClaimPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return codeClaimPayload{}, false
	}
	if strings.TrimSpace(p.Verdict) == "" {
		return codeClaimPayload{}, false
	}
	return p, true
}

func codeClaimFailPrompt(issues []codeClaimIssue) string {
	var b strings.Builder
	b.WriteString("源码声明与 rca_read 原文不符：终答声称的调用/写库在 SOURCE 里不可达，或漏掉了包围它的 if/else/return。请按工具原文改写，不要拼伪源码。\n")
	for _, issue := range issues {
		fmt.Fprintf(&b, "- kind=%s path=%s symbol=%s guard=%s claim=%s\n",
			issue.Kind, issue.Path, issue.Symbol, issue.Guard, issue.Claim)
	}
	return strings.TrimSpace(b.String())
}
