package data

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sixath/framework/memory"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openMemoryUnitTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&MemoryUnit{}); err != nil {
		t.Fatalf("migrate memory units: %v", err)
	}
	return db
}

func TestMemoryUnitRememberAddAndRecall(t *testing.T) {
	backend := NewSessionUnitsBackend(openMemoryUnitTestDB(t))
	ctx := context.Background()

	hit, err := backend.Remember(ctx, memory.RememberInput{
		Scope:    memory.ScopeSession,
		ScopeID:  "session-1",
		AgentID:  "agent-1",
		Action:   memory.ActionAdd,
		Content:  "remember this useful fact",
		Metadata: map[string]any{"kind": "fact"},
	})
	if err != nil {
		t.Fatalf("Remember add: %v", err)
	}
	if hit.ID == "" || hit.Content != "remember this useful fact" {
		t.Fatalf("add hit = %+v", hit)
	}
	if hit.Metadata["content_hash"] != memory.ContentHash(hit.Content) {
		t.Fatalf("content hash = %v", hit.Metadata["content_hash"])
	}
	if hit.Metadata["source_session_id"] != "session-1" || hit.Metadata["agent_id"] != "agent-1" {
		t.Fatalf("add metadata = %+v", hit.Metadata)
	}

	hits, err := backend.Recall(ctx, memory.RecallQuery{
		Scope:   memory.ScopeSession,
		ScopeID: "session-1",
		Query:   "useful",
	})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != hit.ID {
		t.Fatalf("Recall hits = %+v", hits)
	}
}

func TestMemoryUnitReplaceRequiresExistingUnitAndDeleteHidesIt(t *testing.T) {
	backend := NewSessionUnitsBackend(openMemoryUnitTestDB(t))
	ctx := context.Background()

	if _, err := backend.Remember(ctx, memory.RememberInput{
		Scope: memory.ScopeSession, ScopeID: "session-1", Action: memory.ActionReplace, Content: "new",
	}); err == nil {
		t.Fatal("replace without unit ID succeeded")
	}

	created, err := backend.Remember(ctx, memory.RememberInput{
		Scope: memory.ScopeSession, ScopeID: "session-1", Action: memory.ActionAdd, Content: "old",
	})
	if err != nil {
		t.Fatalf("Remember add: %v", err)
	}
	replaced, err := backend.Remember(ctx, memory.RememberInput{
		Scope: memory.ScopeSession, ScopeID: "session-1", UnitID: created.ID, Action: memory.ActionReplace, Content: "new",
	})
	if err != nil {
		t.Fatalf("Remember replace: %v", err)
	}
	if replaced.ID == "" || replaced.ID == created.ID || replaced.Content != "new" {
		t.Fatalf("replace hit = %+v, want new id != %q", replaced, created.ID)
	}
	if got := replaced.Metadata["supersedes_id"]; got != created.ID {
		t.Fatalf("replace supersedes_id = %v, want %q", got, created.ID)
	}

	gotOld, err := backend.Get(ctx, memory.GetRef{Scope: memory.ScopeSession, ScopeID: "session-1", ID: created.ID})
	if err != nil {
		t.Fatalf("Get(old) after replace: %v", err)
	}
	if got := gotOld.Metadata["status"]; got != "superseded" {
		t.Fatalf("old status = %v, want superseded", got)
	}

	if err := backend.Delete(ctx, memory.GetRef{Scope: memory.ScopeSession, ScopeID: "session-1", ID: replaced.ID}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	for _, id := range []string{created.ID, replaced.ID} {
		if _, err := backend.Get(ctx, memory.GetRef{Scope: memory.ScopeSession, ScopeID: "session-1", ID: id}); err == nil {
			t.Fatalf("Get(%s) after cascade delete succeeded", id)
		}
	}
	hits, err := backend.Recall(ctx, memory.RecallQuery{Scope: memory.ScopeSession, ScopeID: "session-1"})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("Recall deleted unit = %+v", hits)
	}
}

func TestSessionUnitsBackend_ReplaceSupersedes(t *testing.T) {
	backend := NewSessionUnitsBackend(openMemoryUnitTestDB(t))
	ctx := context.Background()

	old, err := backend.Remember(ctx, memory.RememberInput{
		Scope: memory.ScopeSession, ScopeID: "session-1", Action: memory.ActionAdd, Content: "v1",
	})
	if err != nil {
		t.Fatalf("Remember(add): %v", err)
	}

	neu, err := backend.Remember(ctx, memory.RememberInput{
		Scope: memory.ScopeSession, ScopeID: "session-1", UnitID: old.ID, Action: memory.ActionReplace, Content: "v2",
	})
	if err != nil {
		t.Fatalf("Remember(replace): %v", err)
	}
	if neu.ID == "" || neu.ID == old.ID {
		t.Fatalf("replace ID = %q, want new ID != old %q", neu.ID, old.ID)
	}
	if neu.Content != "v2" {
		t.Fatalf("replace content = %q, want v2", neu.Content)
	}
	if got := neu.Metadata["supersedes_id"]; got != old.ID {
		t.Fatalf("new supersedes_id = %v, want %q", got, old.ID)
	}
	if got := neu.Metadata["status"]; got != "active" {
		t.Fatalf("new status = %v, want active", got)
	}

	hits, err := backend.Recall(ctx, memory.RecallQuery{
		Scope: memory.ScopeSession, ScopeID: "session-1", Query: "v2",
	})
	if err != nil || len(hits) != 1 || hits[0].ID != neu.ID {
		t.Fatalf("Recall(v2) = %+v err=%v, want only new id", hits, err)
	}
	hitsOld, err := backend.Recall(ctx, memory.RecallQuery{
		Scope: memory.ScopeSession, ScopeID: "session-1", Query: "v1",
	})
	if err != nil || len(hitsOld) != 0 {
		t.Fatalf("Recall(v1) = %+v err=%v, want 0 (superseded)", hitsOld, err)
	}

	gotOld, err := backend.Get(ctx, memory.GetRef{Scope: memory.ScopeSession, ScopeID: "session-1", ID: old.ID})
	if err != nil {
		t.Fatalf("Get(old): %v", err)
	}
	if got := gotOld.Metadata["status"]; got != "superseded" {
		t.Fatalf("old status = %v, want superseded", got)
	}

	// A←B←C chain, then Delete mid/active end cascades whole chain.
	b, err := backend.Remember(ctx, memory.RememberInput{
		Scope: memory.ScopeSession, ScopeID: "session-1", UnitID: neu.ID, Action: memory.ActionReplace, Content: "v3",
	})
	if err != nil {
		t.Fatalf("Remember(replace v3): %v", err)
	}
	if err := backend.Delete(ctx, memory.GetRef{Scope: memory.ScopeSession, ScopeID: "session-1", ID: b.ID}); err != nil {
		t.Fatalf("Delete(active): %v", err)
	}
	for _, id := range []string{old.ID, neu.ID, b.ID} {
		_, err := backend.Get(ctx, memory.GetRef{Scope: memory.ScopeSession, ScopeID: "session-1", ID: id})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("Get(%s) after cascade Delete error = %v, want not found", id, err)
		}
	}
}

func TestSessionUnitsBackend_UserReplaceSupersedes(t *testing.T) {
	backend := NewSessionUnitsBackend(openMemoryUnitTestDB(t))
	ctx := context.Background()

	old, err := backend.Remember(ctx, memory.RememberInput{
		Scope: memory.ScopeUser, ScopeID: "user-1", Action: memory.ActionAdd, Content: "pref-v1",
		Metadata: map[string]any{"source_session_id": "sess-src"},
	})
	if err != nil {
		t.Fatalf("user add: %v", err)
	}
	neu, err := backend.Remember(ctx, memory.RememberInput{
		Scope: memory.ScopeUser, ScopeID: "user-1", UnitID: old.ID, Action: memory.ActionReplace, Content: "pref-v2",
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
	if neu.Metadata["status"] != "active" {
		t.Fatalf("user new status = %v, want active", neu.Metadata["status"])
	}

	hits, err := backend.Recall(ctx, memory.RecallQuery{
		Scope: memory.ScopeUser, ScopeID: "user-1", Query: "pref-v2",
	})
	if err != nil || len(hits) != 1 || hits[0].ID != neu.ID {
		t.Fatalf("user Recall = %+v err=%v", hits, err)
	}
	hitsOld, err := backend.Recall(ctx, memory.RecallQuery{
		Scope: memory.ScopeUser, ScopeID: "user-1", Query: "pref-v1",
	})
	if err != nil || len(hitsOld) != 0 {
		t.Fatalf("user Recall(old) = %+v err=%v, want 0", hitsOld, err)
	}

	gotOld, err := backend.Get(ctx, memory.GetRef{Scope: memory.ScopeUser, ScopeID: "user-1", ID: old.ID})
	if err != nil || gotOld.Metadata["status"] != "superseded" {
		t.Fatalf("user Get(old) = %+v err=%v", gotOld, err)
	}

	if _, err := backend.Remember(ctx, memory.RememberInput{
		Scope: memory.ScopeUser, ScopeID: "user-1", UnitID: neu.ID, Action: memory.ActionRemove,
	}); err != nil {
		t.Fatalf("user remove: %v", err)
	}
	for _, id := range []string{old.ID, neu.ID} {
		_, err := backend.Get(ctx, memory.GetRef{Scope: memory.ScopeUser, ScopeID: "user-1", ID: id})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("user Get(%s) after cascade remove error = %v, want not found", id, err)
		}
	}
}

func TestSessionUnitsBackend_UserScope(t *testing.T) {
	backend := NewSessionUnitsBackend(openMemoryUnitTestDB(t))
	ctx := context.Background()

	hit, err := backend.Remember(ctx, memory.RememberInput{
		Scope:    memory.ScopeUser,
		ScopeID:  "user-1",
		AgentID:  "agent-1",
		Action:   memory.ActionAdd,
		Content:  "prefers UTC",
		Metadata: map[string]any{"source_session_id": "sess-9"},
	})
	if err != nil {
		t.Fatalf("Remember user: %v", err)
	}
	if hit.Scope != memory.ScopeUser || hit.ID == "" {
		t.Fatalf("hit = %+v", hit)
	}
	if hit.Metadata["user_id"] != "user-1" || hit.Metadata["source_session_id"] != "sess-9" {
		t.Fatalf("metadata = %+v", hit.Metadata)
	}

	hits, err := backend.Recall(ctx, memory.RecallQuery{
		Scope: memory.ScopeUser, ScopeID: "user-1", Query: "UTC",
	})
	if err != nil || len(hits) != 1 || hits[0].ID != hit.ID {
		t.Fatalf("user Recall = %+v err=%v", hits, err)
	}

	sessHits, err := backend.Recall(ctx, memory.RecallQuery{
		Scope: memory.ScopeSession, ScopeID: "sess-9", Query: "UTC",
	})
	if err != nil {
		t.Fatalf("session Recall: %v", err)
	}
	if len(sessHits) != 0 {
		t.Fatalf("session Recall should exclude user units, got %+v", sessHits)
	}
}

func TestSessionUnitsBackend_RejectsAgentScope(t *testing.T) {
	backend := NewSessionUnitsBackend(openMemoryUnitTestDB(t))
	_, err := backend.Remember(context.Background(), memory.RememberInput{
		Scope: memory.ScopeAgent, ScopeID: "a1", Action: memory.ActionAdd, Content: "nope",
	})
	if !errors.Is(err, memory.ErrScopeNotEnabled) {
		t.Fatalf("Remember agent scope error = %v, want ErrScopeNotEnabled", err)
	}
}

func TestSessionUnitsBackend_PatchUnitKeepsSameID(t *testing.T) {
	backend := NewSessionUnitsBackend(openMemoryUnitTestDB(t))
	ctx := context.Background()

	old, err := backend.Remember(ctx, memory.RememberInput{
		Scope: memory.ScopeUser, ScopeID: "user-patch", AgentID: "agent-p",
		Action: memory.ActionAdd, Content: "draft-v1",
		Metadata: map[string]any{"hub_status": "draft", "title": "T"},
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	meta := map[string]any{"title": "T"}
	if err := backend.PatchUnit(ctx, memory.GetRef{
		Scope: memory.ScopeUser, ScopeID: "user-patch", ID: old.ID, AgentID: "agent-p",
	}, nil, meta); err != nil {
		t.Fatalf("PatchUnit: %v", err)
	}

	got, err := backend.Get(ctx, memory.GetRef{
		Scope: memory.ScopeUser, ScopeID: "user-patch", ID: old.ID,
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != old.ID {
		t.Fatalf("id changed: got %q want %q", got.ID, old.ID)
	}
	if got.Content != "draft-v1" {
		t.Fatalf("content=%q", got.Content)
	}
	if _, ok := got.Metadata["hub_status"]; ok {
		t.Fatalf("hub_status should be absent: %+v", got.Metadata)
	}
	if got.Metadata["status"] != memoryUnitStatusActive {
		t.Fatalf("status=%v", got.Metadata["status"])
	}

	content := "draft-v2"
	meta2 := map[string]any{"hub_status": "draft", "title": "T2"}
	if err := backend.PatchUnit(ctx, memory.GetRef{
		Scope: memory.ScopeUser, ScopeID: "user-patch", ID: old.ID,
	}, &content, meta2); err != nil {
		t.Fatalf("PatchUnit content: %v", err)
	}
	got2, err := backend.Get(ctx, memory.GetRef{
		Scope: memory.ScopeUser, ScopeID: "user-patch", ID: old.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got2.ID != old.ID || got2.Content != "draft-v2" {
		t.Fatalf("got id=%q content=%q", got2.ID, got2.Content)
	}

	// Contrast: Replace still supersedes with a new row.
	neu, err := backend.Remember(ctx, memory.RememberInput{
		Scope: memory.ScopeUser, ScopeID: "user-patch", UnitID: old.ID,
		Action: memory.ActionReplace, Content: "replaced",
	})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if neu.ID == old.ID {
		t.Fatal("ActionReplace should create a new unit ID")
	}
}
