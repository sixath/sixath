package mea

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRulesAuditor_PathExistsAndContains(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "out.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	aud := RulesAuditor{WorkDir: root}
	c := Contract{
		Round:          1,
		TargetRecordID: "r1",
		AcceptanceChecks: []AcceptanceCheck{
			{Type: "path_exists", Path: "out.txt"},
			{Type: "file_contains", Path: "out.txt", Pattern: "hello"},
		},
	}
	v, err := aud.Audit(context.Background(), TaskState{}, c, ExecutionReport{ClaimComplete: true})
	if err != nil {
		t.Fatal(err)
	}
	if v.Completion != CompletionComplete || v.Integrity != IntegrityClean {
		t.Fatalf("%+v", v)
	}
	if v.ID == "" {
		t.Fatal("expected audit id")
	}
	if len(v.ProposedUpdates) != 1 ||
		v.ProposedUpdates[0].RecordID != "r1" ||
		v.ProposedUpdates[0].Status != StatusCompleted {
		t.Fatalf("proposed updates: %+v", v.ProposedUpdates)
	}
}

func TestRulesAuditor_RejectsFalseClaim(t *testing.T) {
	aud := RulesAuditor{WorkDir: t.TempDir()}
	c := Contract{
		TargetRecordID:   "r1",
		AcceptanceChecks: []AcceptanceCheck{{Type: "path_exists", Path: "missing.txt"}},
	}
	v, err := aud.Audit(context.Background(), TaskState{}, c, ExecutionReport{ClaimComplete: true})
	if err != nil {
		t.Fatal(err)
	}
	if v.Completion == CompletionComplete {
		t.Fatal("must not complete")
	}
	if v.Completion != CompletionIncomplete {
		t.Fatalf("completion=%s", v.Completion)
	}
}

func TestRulesAuditor_JSONPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "meta.json"), []byte(`{"ok":true,"n":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	aud := RulesAuditor{WorkDir: root}
	c := Contract{
		TargetRecordID: "r1",
		AcceptanceChecks: []AcceptanceCheck{
			{Type: "json_path", Path: "meta.json", JSONPath: "ok", Equals: "true"},
		},
	}
	v, err := aud.Audit(context.Background(), TaskState{}, c, ExecutionReport{})
	if err != nil || v.Completion != CompletionComplete {
		t.Fatalf("err=%v v=%+v", err, v)
	}
	if v.Integrity != IntegrityClean {
		t.Fatalf("integrity=%s", v.Integrity)
	}
}

func TestRulesAuditor_RejectPathEscape(t *testing.T) {
	aud := RulesAuditor{WorkDir: t.TempDir()}
	c := Contract{AcceptanceChecks: []AcceptanceCheck{{Type: "path_exists", Path: "../secret"}}}
	v, err := aud.Audit(context.Background(), TaskState{}, c, ExecutionReport{})
	if err != nil {
		t.Fatal(err)
	}
	if v.Completion == CompletionComplete {
		t.Fatal("path escape must fail audit")
	}
	if v.Completion != CompletionIncomplete {
		t.Fatalf("completion=%s", v.Completion)
	}
	if v.Integrity != IntegrityViolation {
		t.Fatalf("integrity=%s want violation", v.Integrity)
	}
}
