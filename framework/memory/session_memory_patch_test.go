package memory

import (
	"context"
	"testing"
)

func TestSessionMemory_PatchUnitKeepsSameID(t *testing.T) {
	m := NewSessionMemory()
	ctx := context.Background()

	hit, err := m.Remember(ctx, RememberInput{
		Scope: ScopeUser, ScopeID: "agent-1", AgentID: "agent-1",
		Action: ActionAdd, Content: "draft-v1",
		Metadata: map[string]any{"hub_status": "draft", "title": "T"},
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	meta := map[string]any{"title": "T"}
	if err := m.PatchUnit(ctx, GetRef{
		Scope: ScopeUser, ScopeID: "agent-1", ID: hit.ID, AgentID: "agent-1",
	}, nil, meta); err != nil {
		t.Fatalf("PatchUnit: %v", err)
	}

	got, err := m.Get(ctx, GetRef{Scope: ScopeUser, ScopeID: "agent-1", ID: hit.ID})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != hit.ID {
		t.Fatalf("id changed: got %q want %q", got.ID, hit.ID)
	}
	if got.Content != "draft-v1" {
		t.Fatalf("content changed unexpectedly: %q", got.Content)
	}
	if _, ok := got.Metadata["hub_status"]; ok {
		t.Fatalf("hub_status should be absent: %+v", got.Metadata)
	}
	if got.Metadata["status"] != unitStatusActive {
		t.Fatalf("status=%v", got.Metadata["status"])
	}
	if _, ok := got.Metadata["supersedes_id"]; ok {
		t.Fatalf("patch must not set supersedes_id: %+v", got.Metadata)
	}

	// Contrast: ActionReplace creates a new ID.
	neu, err := m.Remember(ctx, RememberInput{
		Scope: ScopeUser, ScopeID: "agent-1", AgentID: "agent-1",
		Action: ActionReplace, UnitID: hit.ID, Content: "replaced",
	})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if neu.ID == hit.ID {
		t.Fatal("ActionReplace should create a new unit ID (divergence from PatchUnit)")
	}
}

func TestSessionMemory_PatchUnitUpdatesContentInPlace(t *testing.T) {
	m := NewSessionMemory()
	ctx := context.Background()
	hit, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "v1",
		Metadata: map[string]any{"hub_status": "draft"},
	})
	if err != nil {
		t.Fatal(err)
	}
	content := "v2"
	meta := map[string]any{"hub_status": "draft", "title": "n"}
	if err := m.PatchUnit(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: hit.ID}, &content, meta); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: hit.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != hit.ID || got.Content != "v2" {
		t.Fatalf("got id=%q content=%q", got.ID, got.Content)
	}
	if got.Metadata["content_hash"] != ContentHash("v2") {
		t.Fatalf("hash=%v", got.Metadata["content_hash"])
	}
}
