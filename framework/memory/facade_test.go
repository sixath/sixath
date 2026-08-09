package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFacadeUserScopeRoutesToUnits(t *testing.T) {
	facade := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	ctx := context.Background()
	hit, err := facade.Remember(ctx, RememberInput{
		Scope: ScopeUser, ScopeID: "u1", Action: ActionAdd, Content: "timezone=UTC",
	})
	if err != nil || hit.ID == "" {
		t.Fatalf("Remember user: hit=%+v err=%v", hit, err)
	}
	hits, err := facade.Recall(ctx, RecallQuery{Scope: ScopeUser, ScopeID: "u1", Query: "timezone"})
	if err != nil || len(hits) != 1 {
		t.Fatalf("Recall user: %+v err=%v", hits, err)
	}
}

func TestFacadeRemember_BlocksProceduralKind(t *testing.T) {
	facade := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	_, err := facade.Remember(context.Background(), RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "repair tip",
		Metadata: map[string]any{"kind": "procedural"},
	})
	if !errors.Is(err, ErrProceduralRememberBlocked) {
		t.Fatalf("want ErrProceduralRememberBlocked, got %v", err)
	}
	hits, err := facade.Recall(context.Background(), RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Query: "repair", Limit: 5,
	})
	if err != nil || len(hits) != 0 {
		t.Fatalf("procedural must not persist: hits=%+v err=%v", hits, err)
	}
}

func TestFacadeUserScopeSilentWhenScopeIDEmpty(t *testing.T) {
	facade := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	ctx := context.Background()
	hit, err := facade.Remember(ctx, RememberInput{Scope: ScopeUser, Action: ActionAdd, Content: "x"})
	if err != nil || hit.ID != "" {
		t.Fatalf("silent Remember: hit=%+v err=%v", hit, err)
	}
	hits, err := facade.Recall(ctx, RecallQuery{Scope: ScopeUser, Query: "x"})
	if err != nil || len(hits) != 0 {
		t.Fatalf("silent Recall: %+v err=%v", hits, err)
	}
	list, err := facade.List(ctx, ListFilter{Scope: ScopeUser})
	if err != nil || len(list) != 0 {
		t.Fatalf("silent List: %+v err=%v", list, err)
	}
	if err := facade.Delete(ctx, GetRef{Scope: ScopeUser, ID: "any"}); err != nil {
		t.Fatalf("silent Delete: %v", err)
	}
}

func TestFacadeSessionUnitsCanBeRememberedAndRecalled(t *testing.T) {
	facade := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	ctx := context.Background()

	hit, err := facade.Remember(ctx, RememberInput{
		Scope:   ScopeSession,
		ScopeID: "session-1",
		Action:  ActionAdd,
		Content: "The deployment uses blue green releases.",
	})
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	if hit.ID == "" {
		t.Fatal("Remember() returned empty ID")
	}
	if got := hit.Metadata["source_session_id"]; got != "session-1" {
		t.Fatalf("Remember() source_session_id = %v, want session-1", got)
	}
	if got := hit.Metadata["content_hash"]; got != ContentHash(hit.Content) {
		t.Fatalf("Remember() content_hash = %v, want hash of content", got)
	}

	hits, err := facade.Recall(ctx, RecallQuery{
		Scope:   ScopeSession,
		ScopeID: "session-1",
		Source:  SourceUnits,
		Query:   "BLUE GREEN",
	})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("Recall() returned %d hits, want 1", len(hits))
	}
	if hits[0].ID != hit.ID || !strings.Contains(hits[0].Content, "blue green") {
		t.Fatalf("Recall() hit = %#v, want remembered unit", hits[0])
	}
}

func TestFacadeSessionUnitReplaceRemoveAndGetDeleted(t *testing.T) {
	facade := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	ctx := context.Background()

	hit, err := facade.Remember(ctx, RememberInput{
		Scope:   ScopeSession,
		ScopeID: "session-1",
		Action:  ActionAdd,
		Content: "old content",
	})
	if err != nil {
		t.Fatalf("Remember(add) error = %v", err)
	}

	replaced, err := facade.Remember(ctx, RememberInput{
		Scope:   ScopeSession,
		ScopeID: "session-1",
		Action:  ActionReplace,
		UnitID:  hit.ID,
		Content: "updated content",
	})
	if err != nil {
		t.Fatalf("Remember(replace) error = %v", err)
	}
	if replaced.ID == hit.ID || replaced.Content != "updated content" {
		t.Fatalf("Remember(replace) = %#v, want new ID and updated content", replaced)
	}
	if got := replaced.Metadata["supersedes_id"]; got != hit.ID {
		t.Fatalf("Remember(replace) supersedes_id = %v, want %q", got, hit.ID)
	}

	gotOld, err := facade.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "session-1", ID: hit.ID})
	if err != nil {
		t.Fatalf("Get(old) error = %v, want OK superseded", err)
	}
	if got := gotOld.Metadata["status"]; got != "superseded" {
		t.Fatalf("Get(old) status = %v, want superseded", got)
	}

	hits, err := facade.Recall(ctx, RecallQuery{Scope: ScopeSession, ScopeID: "session-1", Source: SourceUnits, Query: "updated"})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(hits) != 1 || hits[0].ID != replaced.ID {
		t.Fatalf("Recall() = %+v, want only new unit", hits)
	}

	if _, err := facade.Remember(ctx, RememberInput{
		Scope:   ScopeSession,
		ScopeID: "session-1",
		Action:  ActionRemove,
		UnitID:  replaced.ID,
	}); err != nil {
		t.Fatalf("Remember(remove) error = %v", err)
	}
	hits, err = facade.Recall(ctx, RecallQuery{Scope: ScopeSession, ScopeID: "session-1", Source: SourceUnits, Query: "updated"})
	if err != nil {
		t.Fatalf("Recall() after remove error = %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("Recall() returned %d hits after remove, want 0", len(hits))
	}

	for _, id := range []string{hit.ID, replaced.ID} {
		_, err = facade.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "session-1", ID: id})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("Get(%s) error = %v, want not found after cascade delete", id, err)
		}
	}
}

func TestFacadeNilAgentAndTranscriptBackends(t *testing.T) {
	facade := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	ctx := context.Background()

	if _, err := facade.Remember(ctx, RememberInput{Scope: ScopeAgent, Action: ActionAdd, Content: "note"}); err == nil ||
		!strings.Contains(err.Error(), "agent backend not configured") {
		t.Fatalf("Remember(agent) error = %v, want agent backend not configured", err)
	}

	tests := []struct {
		name  string
		query RecallQuery
	}{
		{name: "agent", query: RecallQuery{Scope: ScopeAgent, Source: SourceFiles}},
		{name: "transcript", query: RecallQuery{Scope: ScopeSession, Source: SourceTranscript}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits, err := facade.Recall(ctx, tt.query)
			if err != nil {
				t.Fatalf("Recall() error = %v", err)
			}
			if len(hits) != 0 {
				t.Fatalf("Recall() returned %d hits, want 0", len(hits))
			}
		})
	}
}

func TestFacadeAgentListAndDeleteAreNotSupported(t *testing.T) {
	facade := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	ctx := context.Background()

	_, err := facade.List(ctx, ListFilter{Scope: ScopeAgent})
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("List() error = %v, want ErrNotSupported", err)
	}
	err = facade.Delete(ctx, GetRef{Scope: ScopeAgent, Path: "MEMORY.md"})
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("Delete() error = %v, want ErrNotSupported", err)
	}
}

func TestFacade_ReplaceUsesSupersedeChain(t *testing.T) {
	ctx := context.Background()

	configs := []struct {
		name string
		cfg  FacadeConfig
	}{
		{name: "nil Conflicts defaults", cfg: FacadeConfig{Session: NewSessionMemory()}},
		{name: "explicit StructuralReplaceResolver", cfg: FacadeConfig{
			Session:   NewSessionMemory(),
			Conflicts: StructuralReplaceResolver{},
		}},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			facade := NewFacade(tc.cfg)

			hit, err := facade.Remember(ctx, RememberInput{
				Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "v1",
			})
			if err != nil || hit.ID == "" {
				t.Fatalf("Remember(add): hit=%+v err=%v", hit, err)
			}

			replaced, err := facade.Remember(ctx, RememberInput{
				Scope: ScopeSession, ScopeID: "s1", Action: ActionReplace, UnitID: hit.ID, Content: "v2",
			})
			if err != nil {
				t.Fatalf("Remember(replace) error = %v", err)
			}
			if replaced.ID == "" || replaced.ID == hit.ID {
				t.Fatalf("Remember(replace) ID = %q, want new id (not %q)", replaced.ID, hit.ID)
			}
			if replaced.Content != "v2" {
				t.Fatalf("Remember(replace) content = %q, want v2", replaced.Content)
			}
			if got := replaced.Metadata["supersedes_id"]; got != hit.ID {
				t.Fatalf("supersedes_id = %v, want %q", got, hit.ID)
			}

			old, err := facade.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: hit.ID})
			if err != nil {
				t.Fatalf("Get(old) error = %v, want OK", err)
			}
			if got := old.Metadata["status"]; got != "superseded" {
				t.Fatalf("Get(old) status = %v, want superseded", got)
			}
		})
	}
}

type keepBothResolver struct{}

func (keepBothResolver) Resolve(context.Context, MemoryHit, RememberInput) (ConflictDecision, error) {
	return ConflictKeepBoth, nil
}

func TestFacade_ReplaceKeepBothFailsClosed(t *testing.T) {
	facade := NewFacade(FacadeConfig{
		Session:   NewSessionMemory(),
		Conflicts: keepBothResolver{},
	})
	ctx := context.Background()

	hit, err := facade.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "v1",
	})
	if err != nil {
		t.Fatalf("Remember(add) error = %v", err)
	}

	_, err = facade.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionReplace, UnitID: hit.ID, Content: "v2",
	})
	if err == nil || !strings.Contains(err.Error(), "not allowed for replace") {
		t.Fatalf("Remember(replace) error = %v, want conflict decision not allowed", err)
	}

	got, err := facade.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: hit.ID})
	if err != nil {
		t.Fatalf("Get(original) error = %v", err)
	}
	if got.Content != "v1" {
		t.Fatalf("original content = %q, want unchanged v1", got.Content)
	}
	if st, _ := got.Metadata["status"].(string); st != "" && st != "active" {
		t.Fatalf("original status = %q, want active", st)
	}
}
