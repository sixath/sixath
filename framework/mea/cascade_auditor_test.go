package mea

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCascadeAuditor_RulesVetoBlocksLLM(t *testing.T) {
	work := t.TempDir()
	// Missing out.txt → rules fail; LLM would say complete but must not be consulted.
	llm := LLMAuditor{
		Model: stubModel{reply: `{"completion":"complete","integrity":"clean","summary":"override","evidence":[]}`},
		WorkDir: work,
	}
	a := CascadeAuditor{
		Rules: RulesAuditor{WorkDir: work},
		LLM:   llm,
	}
	c := Contract{
		Round:            1,
		Goal:             "create out.txt",
		Acceptance:       []string{"path_exists:out.txt"},
		AcceptanceChecks: []AcceptanceCheck{{Type: "path_exists", Path: "out.txt"}},
		TargetRecordID:   "r1",
	}
	rep, err := a.Audit(context.Background(), TaskState{}, c, ExecutionReport{ClaimComplete: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Completion == CompletionComplete {
		t.Fatalf("rules veto must not complete: %+v", rep)
	}
	if len(rep.ProposedUpdates) != 0 {
		t.Fatalf("updates=%+v", rep.ProposedUpdates)
	}
	for _, e := range rep.Evidence {
		if e.Excerpt == "override" {
			t.Fatal("LLM must not override rules fail")
		}
	}
}

func TestCascadeAuditor_RulesCompleteSkipsLLM(t *testing.T) {
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "out.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	llm := AuditorFunc(func(ctx context.Context, s TaskState, c Contract, o ExecutionReport) (AuditReport, error) {
		called = true
		return AuditReport{Completion: CompletionIncomplete, Integrity: IntegritySuspect}, nil
	})
	a := CascadeAuditor{
		Rules: RulesAuditor{WorkDir: work},
		LLM:   llm,
	}
	c := Contract{
		Round:            1,
		AcceptanceChecks: []AcceptanceCheck{{Type: "path_exists", Path: "out.txt"}},
		TargetRecordID:   "r1",
	}
	rep, err := a.Audit(context.Background(), TaskState{}, c, ExecutionReport{})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("LLM must not be called when rules complete")
	}
	if rep.Completion != CompletionComplete || rep.Integrity != IntegrityClean {
		t.Fatalf("%+v", rep)
	}
}

func TestCascadeAuditor_LLMPathWhenOnlyAcceptance(t *testing.T) {
	work := t.TempDir()
	a := CascadeAuditor{
		Rules: RulesAuditor{WorkDir: work},
		LLM: LLMAuditor{
			Model: stubModel{reply: `{"completion":"complete","integrity":"clean","summary":"ok","evidence":[{"type":"note","excerpt":"pass"}]}`},
			WorkDir: work,
		},
	}
	c := Contract{
		Round:          1,
		Goal:           "summarize findings",
		Acceptance:     []string{"report is coherent and grounded"},
		TargetRecordID: "r1",
	}
	rep, err := a.Audit(context.Background(), TaskState{Goal: c.Goal}, c, ExecutionReport{Summary: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Completion != CompletionComplete || rep.Integrity != IntegrityClean {
		t.Fatalf("%+v", rep)
	}
	if len(rep.ProposedUpdates) != 1 || rep.ProposedUpdates[0].RecordID != "r1" {
		t.Fatalf("updates=%+v", rep.ProposedUpdates)
	}
}

func TestCascadeAuditor_NoAcceptanceIncomplete(t *testing.T) {
	a := CascadeAuditor{Rules: RulesAuditor{WorkDir: t.TempDir()}}
	rep, err := a.Audit(context.Background(), TaskState{}, Contract{Round: 3}, ExecutionReport{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Completion != CompletionIncomplete || rep.Integrity != IntegritySuspect {
		t.Fatalf("%+v", rep)
	}
}

// AuditorFunc adapts a function to Auditor.
type AuditorFunc func(ctx context.Context, s TaskState, c Contract, o ExecutionReport) (AuditReport, error)

func (f AuditorFunc) Audit(ctx context.Context, s TaskState, c Contract, o ExecutionReport) (AuditReport, error) {
	return f(ctx, s, c, o)
}
