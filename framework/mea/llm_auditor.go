package mea

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/sixath/framework/model"
)

// LLMAuditor asks a Model to judge completion against text acceptance (read-only prompt).
// It does not grant write tools; optional WorkDir listing is included for grounding only.
type LLMAuditor struct {
	Model   model.Model
	WorkDir string
}

var _ Auditor = LLMAuditor{}

type llmAuditPayload struct {
	Completion string `json:"completion"`
	Integrity  string `json:"integrity"`
	Summary    string `json:"summary"`
	Evidence   []struct {
		Type    string `json:"type"`
		Excerpt string `json:"excerpt"`
	} `json:"evidence"`
}

// Audit builds a fresh prompt from goal/state/contract/execution report and parses JSON.
// Empty WorkDir → incomplete + violation (fail-closed). Parse failure → incomplete + suspect.
func (a LLMAuditor) Audit(ctx context.Context, s TaskState, c Contract, o ExecutionReport) (AuditReport, error) {
	report := AuditReport{
		ID:         uuid.NewString(),
		Round:      c.Round,
		Completion: CompletionIncomplete,
		Integrity:  IntegritySuspect,
	}

	if strings.TrimSpace(a.WorkDir) == "" {
		report.Integrity = IntegrityViolation
		report.Evidence = []Evidence{{
			Type:    "workdir",
			Excerpt: "empty workdir",
		}}
		return report, nil
	}
	if a.Model == nil {
		report.Evidence = []Evidence{{
			Type:    "model",
			Excerpt: "nil model",
		}}
		return report, nil
	}

	select {
	case <-ctx.Done():
		return AuditReport{}, ctx.Err()
	default:
	}

	prompt := a.buildPrompt(s, c, o)
	gen, err := a.Model.Chat(ctx, []model.Message{
		{Role: "system", Content: llmAuditorSystemPrompt},
		{Role: "user", Content: prompt},
	})
	if err != nil {
		report.Evidence = []Evidence{{
			Type:    "model",
			Excerpt: err.Error(),
		}}
		return report, nil
	}
	text := ""
	if gen != nil {
		text = gen.Text
	}
	parsed, ok := parseLLMAuditJSON(text)
	if !ok {
		report.Evidence = []Evidence{{
			Type:    "parse",
			Excerpt: "invalid audit JSON",
		}}
		return report, nil
	}

	report.Completion = normalizeCompletion(parsed.Completion)
	report.Integrity = normalizeIntegrity(parsed.Integrity)
	for _, e := range parsed.Evidence {
		report.Evidence = append(report.Evidence, Evidence{
			Type:    e.Type,
			Excerpt: e.Excerpt,
		})
	}
	if parsed.Summary != "" {
		report.Evidence = append(report.Evidence, Evidence{
			Type:    "summary",
			Excerpt: parsed.Summary,
		})
	}

	if report.Completion == CompletionComplete && report.Integrity == IntegrityClean && c.TargetRecordID != "" {
		summary := parsed.Summary
		if summary == "" {
			summary = "llm acceptance passed"
		}
		report.ProposedUpdates = []ProposedUpdate{{
			RecordID: c.TargetRecordID,
			Status:   StatusCompleted,
			Summary:  summary,
		}}
	} else {
		report.ProposedUpdates = nil
		if report.Completion == CompletionComplete && report.Integrity != IntegrityClean {
			report.Completion = CompletionIncomplete
		}
	}
	return report, nil
}

const llmAuditorSystemPrompt = `You are a read-only MEA auditor. Judge whether the execution satisfied the acceptance criteria.
Reply with JSON only (no markdown), shape:
{"completion":"complete|incomplete|blocked","integrity":"clean|suspect|violation","summary":"...","evidence":[{"type":"...","excerpt":"..."}]}
Do not claim complete unless the environment evidence supports it. You have no write tools.`

func (a LLMAuditor) buildPrompt(s TaskState, c Contract, o ExecutionReport) string {
	var b strings.Builder
	b.WriteString("Goal:\n")
	b.WriteString(c.Goal)
	if c.Goal == "" {
		b.WriteString(s.Goal)
	}
	b.WriteString("\n\nAcceptance:\n")
	for _, line := range c.Acceptance {
		fmt.Fprintf(&b, "- %s\n", line)
	}
	b.WriteString("\nTaskState (JSON):\n")
	if raw, err := json.Marshal(s); err == nil {
		b.Write(raw)
	}
	b.WriteString("\n\nContract (JSON):\n")
	if raw, err := json.Marshal(c); err == nil {
		b.Write(raw)
	}
	b.WriteString("\n\nExecutionReport (JSON):\n")
	if raw, err := json.Marshal(o); err == nil {
		b.Write(raw)
	}
	b.WriteString("\n\nWorkDir top-level names (read-only):\n")
	b.WriteString(listWorkDirNames(a.WorkDir))
	b.WriteString("\n")
	return b.String()
}

func listWorkDirNames(workDir string) string {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return "(unreadable: " + err.Error() + ")"
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return "(empty)"
	}
	return strings.Join(names, "\n")
}

func parseLLMAuditJSON(text string) (llmAuditPayload, bool) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return llmAuditPayload{}, false
	}
	// Strip optional ```json fences if the model wraps output.
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```JSON")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSpace(raw)
		if i := strings.LastIndex(raw, "```"); i >= 0 {
			raw = strings.TrimSpace(raw[:i])
		}
	}
	// Extract first JSON object if surrounded by prose.
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var p llmAuditPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return llmAuditPayload{}, false
	}
	if p.Completion == "" && p.Integrity == "" {
		return llmAuditPayload{}, false
	}
	return p, true
}

func normalizeCompletion(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case CompletionComplete:
		return CompletionComplete
	case CompletionBlocked:
		return CompletionBlocked
	default:
		return CompletionIncomplete
	}
}

func normalizeIntegrity(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case IntegrityClean:
		return IntegrityClean
	case IntegrityViolation:
		return IntegrityViolation
	default:
		return IntegritySuspect
	}
}
