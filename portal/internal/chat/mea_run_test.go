package chat

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sixath/framework/mea"
	"github.com/sixath/framework/model"
)

type stubAuditorModel struct{ reply string }

func (s stubAuditorModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: s.reply}, nil
}

func (s stubAuditorModel) Chat(ctx context.Context, messages []model.Message, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: s.reply}, nil
}

func (s stubAuditorModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func TestRunRulesMEA_Disabled(t *testing.T) {
	t.Setenv("SATH_MEA", "0")
	t.Setenv("SATH_MEA_PILOT_AGENTS", "")
	SetMEADataRoot(t.TempDir())

	res, err := RunRulesMEA(context.Background(), RulesMEAInput{
		SessionID: "sess-off",
		AgentID:   "agent-x",
		Goal:      "noop",
		WorkDir:   t.TempDir(),
		Checks:    []mea.AcceptanceCheck{{Type: "path_exists", Path: "out.txt"}},
		Executor: mea.ExecutorFunc(func(ctx context.Context, s mea.TaskState, c mea.Contract) (mea.ExecutionReport, error) {
			t.Fatal("executor must not run when disabled")
			return mea.ExecutionReport{}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped || res.Reason != "disabled" {
		t.Fatalf("got %+v", res)
	}
	// No store writes under data_root/mea
	meaDir := filepath.Join(MEADataRoot(), "mea")
	if entries, err := os.ReadDir(meaDir); err == nil && len(entries) > 0 {
		t.Fatalf("expected no mea store files, got %d", len(entries))
	}
}

func TestRunRulesMEA_FalseClaimThenSuccess(t *testing.T) {
	t.Setenv("SATH_MEA", "1")
	root := t.TempDir()
	SetMEADataRoot(root)
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	var round int
	res, err := RunRulesMEA(context.Background(), RulesMEAInput{
		SessionID: "sess-1",
		AgentID:   "agent-a",
		Goal:      "create out.txt",
		WorkDir:   work,
		Checks:    []mea.AcceptanceCheck{{Type: "path_exists", Path: "out.txt"}},
		Executor: mea.ExecutorFunc(func(ctx context.Context, s mea.TaskState, c mea.Contract) (mea.ExecutionReport, error) {
			round++
			if round == 1 {
				return mea.ExecutionReport{ClaimComplete: true, Summary: "lied"}, nil
			}
			if err := os.WriteFile(filepath.Join(work, "out.txt"), []byte("x"), 0o644); err != nil {
				return mea.ExecutionReport{}, err
			}
			return mea.ExecutionReport{ClaimComplete: true, Summary: "wrote"}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped {
		t.Fatal("expected not skipped")
	}
	if res.Reason != mea.DecisionDone {
		t.Fatalf("reason=%s", res.Reason)
	}
	if len(res.State.Records) == 0 || res.State.Records[0].Status != mea.StatusCompleted {
		t.Fatalf("records=%+v", res.State.Records)
	}
	if round < 2 {
		t.Fatalf("expected false claim then success, rounds=%d", round)
	}
}

func TestRunRulesMEA_TextAcceptanceWithCascadeLLM(t *testing.T) {
	t.Setenv("SATH_MEA", "1")
	root := t.TempDir()
	SetMEADataRoot(root)
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := RunRulesMEA(context.Background(), RulesMEAInput{
		SessionID:  "sess-llm",
		AgentID:    "agent-llm",
		Goal:       "produce a coherent summary",
		WorkDir:    work,
		Acceptance: []string{"summary is grounded"},
		AuditorModel: stubAuditorModel{reply: `{"completion":"complete","integrity":"clean","summary":"ok","evidence":[{"type":"note","excerpt":"pass"}]}`},
		Executor: mea.ExecutorFunc(func(ctx context.Context, s mea.TaskState, c mea.Contract) (mea.ExecutionReport, error) {
			if len(c.AcceptanceChecks) != 0 {
				t.Fatal("expected empty checks for text acceptance")
			}
			return mea.ExecutionReport{ClaimComplete: true, Summary: "wrote summary"}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped {
		t.Fatal("expected not skipped")
	}
	if res.Reason != mea.DecisionDone {
		t.Fatalf("reason=%s", res.Reason)
	}
	if len(res.State.Records) == 0 || res.State.Records[0].Status != mea.StatusCompleted {
		t.Fatalf("records=%+v", res.State.Records)
	}
}

func TestRunRulesMEA_AgentUIFlagWithoutGlobalEnv(t *testing.T) {
	t.Setenv("SATH_MEA", "0")
	t.Setenv("SATH_MEA_PILOT_AGENTS", "")
	root := t.TempDir()
	SetMEADataRoot(root)
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "out.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := RunRulesMEA(context.Background(), RulesMEAInput{
		SessionID:       "sess-ui",
		AgentID:         "agent-ui",
		AgentMEAEnabled: true,
		Goal:            "create out.txt",
		WorkDir:         work,
		Checks:          []mea.AcceptanceCheck{{Type: "path_exists", Path: "out.txt"}},
		Executor: mea.ExecutorFunc(func(ctx context.Context, s mea.TaskState, c mea.Contract) (mea.ExecutionReport, error) {
			return mea.ExecutionReport{ClaimComplete: true, Summary: "ok"}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped {
		t.Fatal("expected UI mea_enabled to run without global env")
	}
	if res.Reason != mea.DecisionDone {
		t.Fatalf("reason=%s", res.Reason)
	}
}

