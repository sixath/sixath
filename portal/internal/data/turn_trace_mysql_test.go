package data

import (
	"context"
	"testing"
	"time"

	"backend/internal/data/model"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/turntrace"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTurnTraceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.TurnTraceRow{}); err != nil {
		t.Fatalf("migrate turn_traces: %v", err)
	}
	return db
}

func sampleTrace(sessionID, agentID, requestID string, calls ...agent.TurnToolCall) *agent.TurnTrace {
	return &agent.TurnTrace{
		SessionID: sessionID,
		AgentID:   agentID,
		RequestID: requestID,
		CreatedAt: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
		Calls:     calls,
	}
}

func TestTurnTraceStore_UpsertSameRequestKeepsTurnSeq(t *testing.T) {
	db := openTurnTraceTestDB(t)
	store := NewTurnTraceStore(db)
	var _ turntrace.Store = store
	ctx := context.Background()

	first := sampleTrace("sess-1", "agent-1", "req-1", agent.TurnToolCall{
		ToolName: "read", ResultPreview: "v1",
	})
	if err := store.Upsert(ctx, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if first.TurnSeq != 1 {
		t.Fatalf("first TurnSeq = %d, want 1", first.TurnSeq)
	}

	second := sampleTrace("sess-1", "agent-1", "req-1", agent.TurnToolCall{
		ToolName: "read", ResultPreview: "v2-updated",
	})
	if err := store.Upsert(ctx, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if second.TurnSeq != 1 {
		t.Fatalf("second TurnSeq = %d, want 1 (unchanged)", second.TurnSeq)
	}

	var count int64
	if err := db.Model(&model.TurnTraceRow{}).Where("session_id = ?", "sess-1").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count = %d, want 1", count)
	}

	got, err := store.GetByRequest(ctx, "sess-1", "req-1")
	if err != nil {
		t.Fatalf("GetByRequest: %v", err)
	}
	if got == nil {
		t.Fatal("GetByRequest returned nil")
	}
	if got.TurnSeq != 1 {
		t.Fatalf("got TurnSeq = %d, want 1", got.TurnSeq)
	}
	if len(got.Calls) != 1 || got.Calls[0].ResultPreview != "v2-updated" {
		t.Fatalf("payload not updated: %+v", got)
	}
}

func TestTurnTraceStore_DifferentRequestsAllocateSeq(t *testing.T) {
	db := openTurnTraceTestDB(t)
	store := NewTurnTraceStore(db)
	ctx := context.Background()

	a := sampleTrace("sess-2", "agent-1", "req-a")
	b := sampleTrace("sess-2", "agent-1", "req-b")
	if err := store.Upsert(ctx, a); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	if err := store.Upsert(ctx, b); err != nil {
		t.Fatalf("upsert b: %v", err)
	}
	if a.TurnSeq != 1 || b.TurnSeq != 2 {
		t.Fatalf("TurnSeq a=%d b=%d, want 1 then 2", a.TurnSeq, b.TurnSeq)
	}

	gotA, err := store.GetByRequest(ctx, "sess-2", "req-a")
	if err != nil {
		t.Fatalf("get a: %v", err)
	}
	gotB, err := store.GetByRequest(ctx, "sess-2", "req-b")
	if err != nil {
		t.Fatalf("get b: %v", err)
	}
	if gotA.TurnSeq != 1 || gotB.TurnSeq != 2 {
		t.Fatalf("stored seq a=%d b=%d", gotA.TurnSeq, gotB.TurnSeq)
	}
}

func TestTurnTraceStore_ListBySessionActiveOnlyOrdered(t *testing.T) {
	db := openTurnTraceTestDB(t)
	store := NewTurnTraceStore(db)
	ctx := context.Background()

	for _, req := range []string{"r1", "r2", "r3"} {
		tr := sampleTrace("sess-3", "agent-1", req)
		if err := store.Upsert(ctx, tr); err != nil {
			t.Fatalf("upsert %s: %v", req, err)
		}
	}
	if err := db.Model(&model.TurnTraceRow{}).
		Where("session_id = ? AND request_id = ?", "sess-3", "r2").
		Update("active", false).Error; err != nil {
		t.Fatalf("deactivate r2: %v", err)
	}

	list, err := store.ListBySession(ctx, "sess-3", 10)
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2 active", len(list))
	}
	if list[0].TurnSeq != 3 || list[0].RequestID != "r3" {
		t.Fatalf("first = seq=%d req=%s, want seq=3 req=r3", list[0].TurnSeq, list[0].RequestID)
	}
	if list[1].TurnSeq != 1 || list[1].RequestID != "r1" {
		t.Fatalf("second = seq=%d req=%s, want seq=1 req=r1", list[1].TurnSeq, list[1].RequestID)
	}
}

func TestTurnTraceStore_DeactivateAfter(t *testing.T) {
	db := openTurnTraceTestDB(t)
	store := NewTurnTraceStore(db)
	ctx := context.Background()

	t0 := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	early := sampleTrace("sess-deact", "agent-1", "early")
	early.CreatedAt = t0
	late := sampleTrace("sess-deact", "agent-1", "late")
	late.CreatedAt = t0.Add(2 * time.Minute)
	if err := store.Upsert(ctx, early); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(ctx, late); err != nil {
		t.Fatal(err)
	}

	reqIDs, err := store.DeactivateAfter(ctx, "sess-deact", t0.Add(time.Minute))
	if err != nil {
		t.Fatalf("DeactivateAfter: %v", err)
	}
	if len(reqIDs) != 1 || reqIDs[0] != "late" {
		t.Fatalf("requestIDs=%v want [late]", reqIDs)
	}
	list, err := store.ListBySession(ctx, "sess-deact", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].RequestID != "early" {
		t.Fatalf("list=%+v want only early", list)
	}
}
