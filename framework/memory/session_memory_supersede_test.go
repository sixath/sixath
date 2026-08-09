package memory

import (
	"context"
	"strings"
	"testing"
)

func TestSessionMemory_ReplaceSupersedesActiveUnit(t *testing.T) {
	m := NewSessionMemory()
	ctx := context.Background()

	old, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "v1",
	})
	if err != nil {
		t.Fatalf("Remember(add) error = %v", err)
	}

	neu, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionReplace, UnitID: old.ID, Content: "v2",
	})
	if err != nil {
		t.Fatalf("Remember(replace) error = %v", err)
	}
	if neu.ID == "" || neu.ID == old.ID {
		t.Fatalf("replace ID = %q, want new ID != old %q", neu.ID, old.ID)
	}
	if neu.Content != "v2" {
		t.Fatalf("replace content = %q, want v2", neu.Content)
	}
	if got := neu.Metadata["supersedes_id"]; got != old.ID {
		t.Fatalf("new metadata supersedes_id = %v, want %q", got, old.ID)
	}
	if got := neu.Metadata["status"]; got != "active" {
		t.Fatalf("new metadata status = %v, want active", got)
	}

	hits, err := m.Recall(ctx, RecallQuery{Scope: ScopeSession, ScopeID: "s1", Query: "v2", Source: SourceUnits})
	if err != nil || len(hits) != 1 || hits[0].ID != neu.ID {
		t.Fatalf("Recall(v2) = %+v err=%v, want only new id", hits, err)
	}
	hitsOld, err := m.Recall(ctx, RecallQuery{Scope: ScopeSession, ScopeID: "s1", Query: "v1", Source: SourceUnits})
	if err != nil || len(hitsOld) != 0 {
		t.Fatalf("Recall(v1) = %+v err=%v, want 0 (superseded)", hitsOld, err)
	}

	gotOld, err := m.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: old.ID})
	if err != nil {
		t.Fatalf("Get(old) error = %v, want OK", err)
	}
	if got := gotOld.Metadata["status"]; got != "superseded" {
		t.Fatalf("old status = %v, want superseded", got)
	}
}

func TestSessionMemory_ReplaceSupersededUnitNotFound(t *testing.T) {
	m := NewSessionMemory()
	ctx := context.Background()

	old, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "v1",
	})
	if err != nil {
		t.Fatalf("Remember(add): %v", err)
	}
	if _, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionReplace, UnitID: old.ID, Content: "v2",
	}); err != nil {
		t.Fatalf("Remember(replace): %v", err)
	}

	_, err = m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionReplace, UnitID: old.ID, Content: "v3",
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("replace superseded error = %v, want not found", err)
	}
}

func TestSessionMemory_CascadeRemoveAlongSupersedeChain(t *testing.T) {
	m := NewSessionMemory()
	ctx := context.Background()

	a, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "A",
	})
	if err != nil {
		t.Fatalf("add A: %v", err)
	}
	b, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionReplace, UnitID: a.ID, Content: "B",
	})
	if err != nil {
		t.Fatalf("replace B: %v", err)
	}
	c, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionReplace, UnitID: b.ID, Content: "C",
	})
	if err != nil {
		t.Fatalf("replace C: %v", err)
	}

	if _, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionRemove, UnitID: c.ID,
	}); err != nil {
		t.Fatalf("remove C: %v", err)
	}

	for _, id := range []string{a.ID, b.ID, c.ID} {
		_, err := m.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: id})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("Get(%s) after remove C error = %v, want not found", id, err)
		}
	}
}

func TestSessionMemory_CascadeDeleteAlongSupersedeChain(t *testing.T) {
	m := NewSessionMemory()
	ctx := context.Background()

	a, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "A",
	})
	if err != nil {
		t.Fatalf("add A: %v", err)
	}
	b, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionReplace, UnitID: a.ID, Content: "B",
	})
	if err != nil {
		t.Fatalf("replace B: %v", err)
	}
	c, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionReplace, UnitID: b.ID, Content: "C",
	})
	if err != nil {
		t.Fatalf("replace C: %v", err)
	}

	if err := m.Delete(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: c.ID}); err != nil {
		t.Fatalf("Delete(C): %v", err)
	}

	for _, id := range []string{a.ID, b.ID, c.ID} {
		_, err := m.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: id})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("Get(%s) after Delete(C) error = %v, want not found", id, err)
		}
	}
}

func TestSessionMemory_CascadeDeleteAlongSupersedeChain_UserScope(t *testing.T) {
	m := NewSessionMemory()
	ctx := context.Background()

	a, err := m.Remember(ctx, RememberInput{
		Scope: ScopeUser, ScopeID: "u1", Action: ActionAdd, Content: "A",
	})
	if err != nil {
		t.Fatalf("add A: %v", err)
	}
	b, err := m.Remember(ctx, RememberInput{
		Scope: ScopeUser, ScopeID: "u1", Action: ActionReplace, UnitID: a.ID, Content: "B",
	})
	if err != nil {
		t.Fatalf("replace B: %v", err)
	}
	c, err := m.Remember(ctx, RememberInput{
		Scope: ScopeUser, ScopeID: "u1", Action: ActionReplace, UnitID: b.ID, Content: "C",
	})
	if err != nil {
		t.Fatalf("replace C: %v", err)
	}

	if err := m.Delete(ctx, GetRef{Scope: ScopeUser, ScopeID: "u1", ID: c.ID}); err != nil {
		t.Fatalf("Delete(C): %v", err)
	}

	for _, id := range []string{a.ID, b.ID, c.ID} {
		_, err := m.Get(ctx, GetRef{Scope: ScopeUser, ScopeID: "u1", ID: id})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("Get(%s) after Delete(C) error = %v, want not found", id, err)
		}
	}
}

func TestSessionMemory_GetSupersededOKDeletedNotFound(t *testing.T) {
	m := NewSessionMemory()
	ctx := context.Background()

	old, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "v1",
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	neu, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionReplace, UnitID: old.ID, Content: "v2",
	})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}

	got, err := m.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: old.ID})
	if err != nil {
		t.Fatalf("Get(superseded) error = %v, want OK", err)
	}
	if got.Metadata["status"] != "superseded" {
		t.Fatalf("Get(superseded) status = %v", got.Metadata["status"])
	}

	if _, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionRemove, UnitID: neu.ID,
	}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	_, err = m.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: neu.ID})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Get(deleted) error = %v, want not found", err)
	}
	_, err = m.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: old.ID})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Get(cascaded deleted ancestor) error = %v, want not found", err)
	}
}

func TestSessionMemory_ReplaceAndCascadeRemove_UserScope(t *testing.T) {
	m := NewSessionMemory()
	ctx := context.Background()

	old, err := m.Remember(ctx, RememberInput{
		Scope: ScopeUser, ScopeID: "u1", Action: ActionAdd, Content: "pref-v1",
		Metadata: map[string]any{"source_session_id": "s-src"},
	})
	if err != nil {
		t.Fatalf("user add: %v", err)
	}
	neu, err := m.Remember(ctx, RememberInput{
		Scope: ScopeUser, ScopeID: "u1", Action: ActionReplace, UnitID: old.ID, Content: "pref-v2",
	})
	if err != nil {
		t.Fatalf("user replace: %v", err)
	}
	if neu.ID == old.ID {
		t.Fatalf("user replace kept same ID %q", neu.ID)
	}
	if neu.Metadata["supersedes_id"] != old.ID {
		t.Fatalf("user supersedes_id = %v, want %q", neu.Metadata["supersedes_id"], old.ID)
	}

	hits, err := m.Recall(ctx, RecallQuery{Scope: ScopeUser, ScopeID: "u1", Query: "pref-v2", Source: SourceUnits})
	if err != nil || len(hits) != 1 || hits[0].ID != neu.ID {
		t.Fatalf("user Recall = %+v err=%v", hits, err)
	}

	gotOld, err := m.Get(ctx, GetRef{Scope: ScopeUser, ScopeID: "u1", ID: old.ID})
	if err != nil || gotOld.Metadata["status"] != "superseded" {
		t.Fatalf("user Get(old) = %+v err=%v", gotOld, err)
	}

	if _, err := m.Remember(ctx, RememberInput{
		Scope: ScopeUser, ScopeID: "u1", Action: ActionRemove, UnitID: neu.ID,
	}); err != nil {
		t.Fatalf("user remove: %v", err)
	}
	for _, id := range []string{old.ID, neu.ID} {
		_, err := m.Get(ctx, GetRef{Scope: ScopeUser, ScopeID: "u1", ID: id})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("user Get(%s) after cascade remove error = %v, want not found", id, err)
		}
	}
}

func TestSessionMemory_ReplaceAndCascadeRemove_SessionScope(t *testing.T) {
	m := NewSessionMemory()
	ctx := context.Background()

	old, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "sess-v1",
	})
	if err != nil {
		t.Fatalf("session add: %v", err)
	}
	neu, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionReplace, UnitID: old.ID, Content: "sess-v2",
	})
	if err != nil {
		t.Fatalf("session replace: %v", err)
	}
	if neu.ID == old.ID || neu.Metadata["supersedes_id"] != old.ID {
		t.Fatalf("session replace = %#v, want new id superseding %q", neu, old.ID)
	}

	if _, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionRemove, UnitID: neu.ID,
	}); err != nil {
		t.Fatalf("session remove: %v", err)
	}
	for _, id := range []string{old.ID, neu.ID} {
		_, err := m.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: id})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("session Get(%s) after cascade error = %v, want not found", id, err)
		}
	}
}
