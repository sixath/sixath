package mea

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sixath/framework/model"
)

type stubModel struct{ reply string }

func (s stubModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: s.reply}, nil
}

func (s stubModel) Chat(ctx context.Context, messages []model.Message, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: s.reply}, nil
}

func (s stubModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func TestLLMAuditor_CompleteWithTarget(t *testing.T) {
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "out.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := LLMAuditor{
		Model: stubModel{reply: `{"completion":"complete","integrity":"clean","summary":"looks good","evidence":[{"type":"file","excerpt":"out.txt present"}]}`},
		WorkDir: work,
	}
	c := Contract{
		Round:          1,
		Goal:           "create out.txt",
		Acceptance:     []string{"out.txt exists with useful content"},
		TargetRecordID: "r1",
	}
	rep, err := a.Audit(context.Background(), TaskState{Goal: c.Goal}, c, ExecutionReport{Summary: "wrote file"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Completion != CompletionComplete || rep.Integrity != IntegrityClean {
		t.Fatalf("%+v", rep)
	}
	if len(rep.ProposedUpdates) != 1 || rep.ProposedUpdates[0].RecordID != "r1" ||
		rep.ProposedUpdates[0].Status != StatusCompleted {
		t.Fatalf("updates=%+v", rep.ProposedUpdates)
	}
}

func TestLLMAuditor_ParseFailure(t *testing.T) {
	a := LLMAuditor{Model: stubModel{reply: "not json"}, WorkDir: t.TempDir()}
	rep, err := a.Audit(context.Background(), TaskState{}, Contract{Round: 2}, ExecutionReport{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Completion != CompletionIncomplete || rep.Integrity != IntegritySuspect {
		t.Fatalf("%+v", rep)
	}
	if len(rep.ProposedUpdates) != 0 {
		t.Fatalf("updates=%+v", rep.ProposedUpdates)
	}
}

func TestLLMAuditor_EmptyWorkDir(t *testing.T) {
	a := LLMAuditor{Model: stubModel{reply: `{"completion":"complete","integrity":"clean"}`}, WorkDir: ""}
	rep, err := a.Audit(context.Background(), TaskState{}, Contract{Round: 1, TargetRecordID: "r1"}, ExecutionReport{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Completion != CompletionIncomplete || rep.Integrity != IntegrityViolation {
		t.Fatalf("%+v", rep)
	}
	if len(rep.ProposedUpdates) != 0 {
		t.Fatal("must not propose updates")
	}
}
