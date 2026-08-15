package mea

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapManager_NoChecksAsk(t *testing.T) {
	mgr := BootstrapManager{Goal: "x", Checks: nil}
	d, c, _, err := mgr.Decide(context.Background(), TaskState{SessionID: "s", Goal: "x"})
	if err != nil && !errors.Is(err, ErrNoObservableAcceptance) {
		t.Fatalf("unexpected err: %v", err)
	}
	if d == DecisionExecute || c != nil {
		t.Fatal("must not execute without observable acceptance")
	}
	if d != DecisionAsk && err == nil {
		t.Fatal("expected ask or error")
	}
}

func TestBootstrapManager_TextAcceptanceExecute(t *testing.T) {
	mgr := BootstrapManager{
		Goal:       "write a coherent summary",
		Acceptance: []string{"summary is grounded in evidence"},
	}
	d, c, state, err := mgr.Decide(context.Background(), TaskState{SessionID: "s", Goal: mgr.Goal})
	if err != nil {
		t.Fatal(err)
	}
	if d != DecisionExecute || c == nil {
		t.Fatalf("decision=%s contract=%v", d, c)
	}
	if len(c.AcceptanceChecks) != 0 {
		t.Fatalf("checks must be empty: %+v", c.AcceptanceChecks)
	}
	if len(c.Acceptance) != 1 || c.Acceptance[0] != "summary is grounded in evidence" {
		t.Fatalf("acceptance=%v", c.Acceptance)
	}
	if c.TargetRecordID == "" || len(state.Records) != 1 {
		t.Fatalf("target=%q records=%+v", c.TargetRecordID, state.Records)
	}
}

func TestOrchestrator_FalseClaimThenSuccess(t *testing.T) {
	dir := t.TempDir()
	meaDir := filepath.Join(dir, "mea")
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(meaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(meaDir)
	checks := []AcceptanceCheck{{Type: "path_exists", Path: "out.txt"}}
	mgr := BootstrapManager{Goal: "create out.txt", Checks: checks}
	var round int
	exec := ExecutorFunc(func(ctx context.Context, s TaskState, c Contract) (ExecutionReport, error) {
		round++
		if round == 1 {
			return ExecutionReport{ClaimComplete: true, Summary: "lied"}, nil
		}
		if err := os.WriteFile(filepath.Join(work, "out.txt"), []byte("x"), 0o644); err != nil {
			return ExecutionReport{}, err
		}
		return ExecutionReport{ClaimComplete: true, Summary: "wrote"}, nil
	})
	orch := Orchestrator{
		Store: store, Manager: mgr, Executor: exec,
		Auditor: RulesAuditor{WorkDir: work}, MaxRounds: 25,
	}
	final, reason, err := orch.Run(context.Background(), RunInput{
		SessionID: "sess-1", AgentID: "a", Goal: "create out.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reason != DecisionDone {
		t.Fatalf("reason=%s", reason)
	}
	if len(final.Records) == 0 || final.Records[0].Status != StatusCompleted {
		t.Fatalf("records=%+v", final.Records)
	}
}

func TestOrchestrator_FalseClaimVariants(t *testing.T) {
	tests := []struct {
		name   string
		checks []AcceptanceCheck
		write  func(work string) error
	}{
		{
			name:   "missing_path",
			checks: []AcceptanceCheck{{Type: "path_exists", Path: "out.txt"}},
			write:  nil, // never create
		},
		{
			name: "wrong_contains",
			checks: []AcceptanceCheck{
				{Type: "file_contains", Path: "out.txt", Pattern: "hello"},
			},
			write: func(work string) error {
				return os.WriteFile(filepath.Join(work, "out.txt"), []byte("goodbye"), 0o644)
			},
		},
		{
			name: "wrong_json",
			checks: []AcceptanceCheck{
				{Type: "json_path", Path: "meta.json", JSONPath: "ok", Equals: "true"},
			},
			write: func(work string) error {
				return os.WriteFile(filepath.Join(work, "meta.json"), []byte(`{"ok":false}`), 0o644)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			meaDir := filepath.Join(dir, "mea")
			work := filepath.Join(dir, "work")
			if err := os.MkdirAll(meaDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(work, 0o755); err != nil {
				t.Fatal(err)
			}
			if tt.write != nil {
				if err := tt.write(work); err != nil {
					t.Fatal(err)
				}
			}
			store := NewFileStore(meaDir)
			mgr := BootstrapManager{Goal: "g", Checks: tt.checks}
			exec := ExecutorFunc(func(ctx context.Context, s TaskState, c Contract) (ExecutionReport, error) {
				return ExecutionReport{ClaimComplete: true, Summary: "false claim"}, nil
			})
			orch := Orchestrator{
				Store: store, Manager: mgr, Executor: exec,
				Auditor: RulesAuditor{WorkDir: work}, MaxRounds: 3,
			}
			final, reason, err := orch.Run(context.Background(), RunInput{
				SessionID: "sess-" + tt.name, AgentID: "a", Goal: "g",
			})
			if err != nil {
				t.Fatal(err)
			}
			if reason == DecisionDone {
				t.Fatalf("must not be done; reason=%s", reason)
			}
			if reason != ReasonMaxRounds && reason != DecisionAsk && reason != DecisionBlocked {
				// still pending after limited rounds is also OK via max_rounds
				t.Logf("reason=%s", reason)
			}
			if len(final.Records) > 0 && final.Records[0].Status == StatusCompleted {
				t.Fatalf("must not complete: %+v", final.Records[0])
			}
			if reason != ReasonMaxRounds {
				// Spec: max_rounds or still pending after limited rounds
				pending := false
				for _, r := range final.Records {
					if r.Status == StatusPending {
						pending = true
						break
					}
				}
				if !pending && reason != ReasonMaxRounds {
					t.Fatalf("expected max_rounds or pending; reason=%s records=%+v", reason, final.Records)
				}
			}
		})
	}
}

// sequentialManager issues execute contracts for pending records in order with per-record checks.
type sequentialManager struct {
	checksByRecord map[string][]AcceptanceCheck
}

func (m sequentialManager) Decide(ctx context.Context, s TaskState) (string, *Contract, TaskState, error) {
	select {
	case <-ctx.Done():
		return "", nil, s, ctx.Err()
	default:
	}
	hasBlocked := false
	for _, r := range s.Records {
		if r.Status == StatusBlocked {
			hasBlocked = true
		}
		if r.Status != StatusPending {
			continue
		}
		checks := append([]AcceptanceCheck(nil), m.checksByRecord[r.ID]...)
		c := &Contract{
			Round:            len(s.Audits) + 1,
			Goal:             s.Goal,
			Acceptance:       acceptanceStrings(checks),
			AcceptanceChecks: checks,
			TargetRecordID:   r.ID,
			RelevantStateIDs: []string{r.ID},
		}
		return DecisionExecute, c, s, nil
	}
	if hasBlocked {
		return DecisionBlocked, nil, s, nil
	}
	return DecisionDone, nil, s, nil
}

func TestOrchestrator_TwoRequirementsSequential(t *testing.T) {
	dir := t.TempDir()
	meaDir := filepath.Join(dir, "mea")
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(meaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	store := NewFileStore(meaDir)
	state := TaskState{
		Version:   1,
		SessionID: "sess-two",
		AgentID:   "a",
		Goal:      "two files",
		Records: []TaskRecord{
			{ID: "r1", Kind: KindRequirement, Status: StatusPending, Summary: "a.txt"},
			{ID: "r2", Kind: KindRequirement, Status: StatusPending, Summary: "b.txt"},
		},
	}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}

	mgr := sequentialManager{checksByRecord: map[string][]AcceptanceCheck{
		"r1": {{Type: "path_exists", Path: "a.txt"}},
		"r2": {{Type: "path_exists", Path: "b.txt"}},
	}}
	exec := ExecutorFunc(func(ctx context.Context, s TaskState, c Contract) (ExecutionReport, error) {
		path := ""
		for _, ch := range c.AcceptanceChecks {
			if ch.Type == "path_exists" {
				path = ch.Path
				break
			}
		}
		if path == "" {
			return ExecutionReport{}, errors.New("no path in contract")
		}
		if err := os.WriteFile(filepath.Join(work, path), []byte("ok"), 0o644); err != nil {
			return ExecutionReport{}, err
		}
		return ExecutionReport{ClaimComplete: true, Summary: "wrote " + path}, nil
	})
	orch := Orchestrator{
		Store: store, Manager: mgr, Executor: exec,
		Auditor: RulesAuditor{WorkDir: work}, MaxRounds: 10,
	}
	final, reason, err := orch.Run(context.Background(), RunInput{
		SessionID: "sess-two", AgentID: "a", Goal: "two files",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reason != DecisionDone {
		t.Fatalf("reason=%s", reason)
	}
	if len(final.Records) != 2 {
		t.Fatalf("records=%d", len(final.Records))
	}
	for _, r := range final.Records {
		if r.Status != StatusCompleted {
			t.Fatalf("record %s status=%s", r.ID, r.Status)
		}
	}
}
