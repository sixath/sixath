package memory

import (
	"context"
	"errors"
	"testing"
)

func TestFacade_AddHashDedupeSkipsWrite(t *testing.T) {
	stub := &StubSemanticConflictResolver{Decision: ConflictKeepBoth}
	f := NewFacade(FacadeConfig{
		Session:              NewSessionMemory(),
		SemanticConflicts:    stub,
		ToolSemanticConflict: true,
	})
	ctx := context.Background()
	content := "same fact hash dedupe"

	first, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: content,
	})
	if err != nil || first.ID == "" {
		t.Fatalf("first add: hit=%+v err=%v", first, err)
	}
	callsAfterFirst := stub.Calls

	second, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: content,
	})
	if err != nil {
		t.Fatalf("second add error = %v", err)
	}
	if second.ID != "" {
		t.Fatalf("second add ID = %q, want empty (hash skip)", second.ID)
	}
	if stub.Calls != callsAfterFirst {
		t.Fatalf("stub.Calls = %d, want %d (no resolver on hash skip)", stub.Calls, callsAfterFirst)
	}
}

func TestFacade_ToolSemanticConflictOff_DoesNotCallResolver(t *testing.T) {
	stub := &StubSemanticConflictResolver{Decision: ConflictIgnore}
	f := NewFacade(FacadeConfig{
		Session:              NewSessionMemory(),
		SemanticConflicts:    stub,
		ToolSemanticConflict: false,
	})
	ctx := context.Background()

	hit, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "tool path fact",
	})
	if err != nil {
		t.Fatalf("Remember error = %v", err)
	}
	if hit.ID == "" {
		t.Fatal("expected write success with non-empty ID")
	}
	if stub.Calls != 0 {
		t.Fatalf("stub.Calls = %d, want 0 when ToolSemanticConflict=false", stub.Calls)
	}
}

func TestFacade_SemanticConflictsNil_DirectAdd(t *testing.T) {
	f := NewFacade(FacadeConfig{
		Session:              NewSessionMemory(),
		SemanticConflicts:    nil,
		ToolSemanticConflict: true,
	})
	ctx := context.Background()

	hit, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd,
		Content: "nil resolver direct add",
		Metadata: map[string]any{"source": "turn_extract"},
	})
	if err != nil {
		t.Fatalf("Remember error = %v", err)
	}
	if hit.ID == "" {
		t.Fatal("expected direct add with non-empty ID")
	}
}

func TestFacade_EmptyPeers_DirectAddNoResolverCall(t *testing.T) {
	stub := &StubSemanticConflictResolver{Decision: ConflictIgnore}
	f := NewFacade(FacadeConfig{
		Session:              NewSessionMemory(),
		SemanticConflicts:    stub,
		ToolSemanticConflict: true,
	})
	ctx := context.Background()

	if _, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "alpha beta gamma",
	}); err != nil {
		t.Fatalf("seed add error = %v", err)
	}
	callsAfterSeed := stub.Calls

	// Completely different string: no substring overlap with seed → Recall peers empty.
	hit, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "xyzzy quux",
	})
	if err != nil {
		t.Fatalf("Remember error = %v", err)
	}
	if hit.ID == "" {
		t.Fatal("expected direct add when peers empty")
	}
	if stub.Calls != callsAfterSeed {
		t.Fatalf("stub.Calls = %d, want %d (no ResolveAdd when peers empty)", stub.Calls, callsAfterSeed)
	}
}

func TestFacade_TurnExtractSource_CallsResolver(t *testing.T) {
	stub := &StubSemanticConflictResolver{Decision: ConflictKeepBoth}
	f := NewFacade(FacadeConfig{
		Session:              NewSessionMemory(),
		SemanticConflicts:    stub,
		ToolSemanticConflict: false,
	})
	ctx := context.Background()

	if _, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "favorite color is blue",
	}); err != nil {
		t.Fatalf("seed error = %v", err)
	}

	// "color" is a substring of the seed so LIKE/Contains peer Recall is non-empty.
	hit, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "color",
		Metadata: map[string]any{"source": "turn_extract"},
	})
	if err != nil {
		t.Fatalf("Remember error = %v", err)
	}
	if hit.ID == "" {
		t.Fatal("KeepBoth should write")
	}
	if stub.Calls < 1 {
		t.Fatalf("stub.Calls = %d, want >= 1 for turn_extract", stub.Calls)
	}
}

func TestFacade_KeepBothAddsSecondActive(t *testing.T) {
	stub := &StubSemanticConflictResolver{Decision: ConflictKeepBoth}
	f := NewFacade(FacadeConfig{
		Session:              NewSessionMemory(),
		SemanticConflicts:    stub,
		ToolSemanticConflict: true,
	})
	ctx := context.Background()

	if _, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "favorite color is blue",
	}); err != nil {
		t.Fatalf("first add error = %v", err)
	}

	// Candidate content must be a substring of an existing unit for SessionMemory Contains/LIKE peers.
	second, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "favorite color",
	})
	if err != nil || second.ID == "" {
		t.Fatalf("second add: hit=%+v err=%v", second, err)
	}
	if stub.Calls < 1 {
		t.Fatalf("stub.Calls = %d, want >= 1", stub.Calls)
	}

	hits, err := f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits, Query: "color",
	})
	if err != nil {
		t.Fatalf("Recall error = %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("Recall hits = %d, want 2 active (KeepBoth)", len(hits))
	}
}

func TestFacade_SupersedeViaSemanticAdd(t *testing.T) {
	sm := NewSessionMemory()
	stub := &StubSemanticConflictResolver{Decision: ConflictSupersede}
	f := NewFacade(FacadeConfig{
		Session:              sm,
		SemanticConflicts:    stub,
		ToolSemanticConflict: true,
	})
	ctx := context.Background()

	old, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "favorite color is blue",
	})
	if err != nil || old.ID == "" {
		t.Fatalf("first add: hit=%+v err=%v", old, err)
	}
	stub.TargetUnitID = old.ID

	neu, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "favorite color",
	})
	if err != nil {
		t.Fatalf("semantic supersede add error = %v", err)
	}
	if neu.ID == "" || neu.ID == old.ID {
		t.Fatalf("new ID = %q, want fresh id (not %q)", neu.ID, old.ID)
	}
	if stub.Calls < 1 {
		t.Fatalf("stub.Calls = %d, want >= 1", stub.Calls)
	}

	gotOld, err := f.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: old.ID})
	if err != nil {
		t.Fatalf("Get(old) error = %v", err)
	}
	if st, _ := gotOld.Metadata["status"].(string); st != "superseded" {
		t.Fatalf("old status = %q, want superseded", st)
	}
	if got := neu.Metadata["supersedes_id"]; got != old.ID {
		t.Fatalf("supersedes_id = %v, want %q", got, old.ID)
	}
}

func TestFacade_SemanticLLMErrorSkipsWrite(t *testing.T) {
	stub := &StubSemanticConflictResolver{Err: errors.New("llm boom")}
	f := NewFacade(FacadeConfig{
		Session:              NewSessionMemory(),
		SemanticConflicts:    stub,
		ToolSemanticConflict: true,
	})
	ctx := context.Background()

	if _, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "favorite color is blue",
	}); err != nil {
		t.Fatalf("seed error = %v", err)
	}

	before, err := f.List(ctx, ListFilter{Scope: ScopeSession, ScopeID: "s1", Status: "active"})
	if err != nil {
		t.Fatalf("List before error = %v", err)
	}

	hit, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "color",
	})
	if err != nil {
		t.Fatalf("Remember error = %v, want nil (fail-closed)", err)
	}
	if hit.ID != "" {
		t.Fatalf("hit.ID = %q, want empty on LLM error", hit.ID)
	}

	after, err := f.List(ctx, ListFilter{Scope: ScopeSession, ScopeID: "s1", Status: "active"})
	if err != nil {
		t.Fatalf("List after error = %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("active count = %d, want %d (no new row)", len(after), len(before))
	}
}

func TestFacade_InvalidSupersedeTargetSkipsWrite(t *testing.T) {
	stub := &StubSemanticConflictResolver{
		Decision:     ConflictSupersede,
		TargetUnitID: "not-a-peer-id",
	}
	f := NewFacade(FacadeConfig{
		Session:              NewSessionMemory(),
		SemanticConflicts:    stub,
		ToolSemanticConflict: true,
	})
	ctx := context.Background()

	if _, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "favorite color is blue",
	}); err != nil {
		t.Fatalf("seed error = %v", err)
	}
	before, err := f.List(ctx, ListFilter{Scope: ScopeSession, ScopeID: "s1", Status: "active"})
	if err != nil {
		t.Fatalf("List before error = %v", err)
	}

	hit, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "color",
	})
	if err != nil {
		t.Fatalf("Remember error = %v, want nil", err)
	}
	if hit.ID != "" {
		t.Fatalf("hit.ID = %q, want empty for invalid supersede target", hit.ID)
	}

	after, err := f.List(ctx, ListFilter{Scope: ScopeSession, ScopeID: "s1", Status: "active"})
	if err != nil {
		t.Fatalf("List after error = %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("active count = %d, want %d", len(after), len(before))
	}
}

func TestFacade_ReplaceStillStructural(t *testing.T) {
	stub := &StubSemanticConflictResolver{Decision: ConflictIgnore}
	f := NewFacade(FacadeConfig{
		Session:              NewSessionMemory(),
		SemanticConflicts:    stub,
		ToolSemanticConflict: true,
	})
	ctx := context.Background()

	old, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "v1",
	})
	if err != nil {
		t.Fatalf("add error = %v", err)
	}
	callsAfterAdd := stub.Calls

	replaced, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionReplace, UnitID: old.ID, Content: "v2",
	})
	if err != nil {
		t.Fatalf("replace error = %v", err)
	}
	if replaced.ID == "" || replaced.ID == old.ID {
		t.Fatalf("replace ID = %q, want new supersede id", replaced.ID)
	}
	if stub.Calls != callsAfterAdd {
		t.Fatalf("stub.Calls = %d, want %d (replace must not call SemanticConflicts)", stub.Calls, callsAfterAdd)
	}

	gotOld, err := f.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: old.ID})
	if err != nil {
		t.Fatalf("Get(old) error = %v", err)
	}
	if st, _ := gotOld.Metadata["status"].(string); st != "superseded" {
		t.Fatalf("old status = %q, want superseded", st)
	}
}
