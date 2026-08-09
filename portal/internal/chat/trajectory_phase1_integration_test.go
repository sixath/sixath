package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/data"
	"backend/internal/data/model"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/sessionsearch"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestTrajectoryPhase1_PersistFTSSearchAnchored is the Task 14 Step 1 smoke:
// RunTrace → MySQL TurnTrace store → FTS tool projection → SearchAnchored hit
// → memory SessionTranscript recall includes role=tool.
func TestTrajectoryPhase1_PersistFTSSearchAnchored(t *testing.T) {
	t.Setenv("SATH_TRACE_PERSIST", "true")
	dir := t.TempDir()
	agentID := "e2e-phase1-agent"
	sessionID := "e2e-phase1-sess"
	requestID := "e2e-phase1-req"

	prev := DefaultSessionSearchConfig
	DefaultSessionSearchConfig = config.SessionSearchConfig{Enabled: true, StoreDir: dir}
	t.Cleanup(func() {
		DefaultSessionSearchConfig = prev
		sessionsearch.ResetManagerCacheForTest()
	})

	cfg := config.Config{SessionSearch: DefaultSessionSearchConfig}
	mgr, err := sessionsearch.GetSessionSearchManager(cfg, agentID)
	if err != nil || mgr == nil {
		t.Fatalf("GetSessionSearchManager: mgr=%v err=%v", mgr, err)
	}

	db, err := gorm.Open(sqlite.Open("file:phase1_e2e?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.TurnTraceRow{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := data.NewTurnTraceStore(db)

	base := time.Date(2026, 8, 5, 21, 0, 0, 0, time.UTC)
	sessMeta := sessionsearch.SessionMeta{
		ID: sessionID, AgentID: agentID, Title: "phase1 e2e", UpdatedAt: base,
	}
	surrounding := []sessionsearch.MessageDoc{
		{ID: "m0", SessionID: sessionID, Role: "user", Content: "please inspect the local config file", CreatedAt: base},
		{ID: "m1", SessionID: sessionID, Role: "assistant", Content: "I will read it now", CreatedAt: base.Add(time.Second)},
	}
	for _, msg := range surrounding {
		if err := mgr.IndexMessage(context.Background(), sessMeta, msg); err != nil {
			t.Fatalf("index surrounding: %v", err)
		}
	}

	tr := &agent.RunTrace{
		RequestID: requestID,
		ToolCalls: []agent.ToolCallRecord{
			{
				ToolCallID: "tc-read",
				ToolName:   "execute_read",
				Arguments:  map[string]any{"path": "/tmp/phase1.conf"},
				Result:     "ok-content",
			},
		},
	}
	PersistTurnTraceIfEnabled(context.Background(), store, agent.TurnTraceMeta{
		SessionID: sessionID,
		AgentID:   agentID,
		RequestID: requestID,
	}, tr, &TurnTraceIndexOpts{Manager: mgr, SessMeta: sessMeta})

	got, err := store.GetByRequest(context.Background(), sessionID, requestID)
	if err != nil || got == nil {
		t.Fatalf("GetByRequest: got=%v err=%v", got, err)
	}
	if len(got.Calls) != 1 || got.Calls[0].ToolName != "execute_read" {
		t.Fatalf("persisted calls=%+v", got.Calls)
	}

	backend := NewSessionSearchBackendWithManager(nil, func(_ context.Context, id string) (sessionsearch.SessionSearchManager, error) {
		if id != agentID {
			t.Fatalf("agentID=%q", id)
		}
		return mgr, nil
	})
	// Query unique tool-projection tokens (path/result), not the tool name alone —
	// assistant chatter must not steal the FTS anchor.
	hits, err := backend.SearchAnchored(context.Background(), biz.TranscriptSearchOpts{
		AgentID:      agentID,
		Query:        "phase1.conf ok-content",
		IncludeTools: true,
		Window:       2,
	})
	if err != nil {
		t.Fatalf("SearchAnchored: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits=%d want 1: %+v", len(hits), hits)
	}
	if hits[0].Anchor.Role != "tool" || hits[0].Anchor.ToolName != "execute_read" {
		t.Fatalf("anchor=%+v", hits[0].Anchor)
	}
	if hits[0].Anchor.ID != "trace:"+requestID+":tc-read" {
		t.Fatalf("anchor.id=%q", hits[0].Anchor.ID)
	}
	windowHasTool := false
	for _, m := range hits[0].Window {
		if m.Role == "tool" && m.ToolName == "execute_read" {
			windowHasTool = true
		}
	}
	if !windowHasTool {
		t.Fatalf("window missing tool role: %+v", hits[0].Window)
	}

	transcript := memory.NewSessionTranscript(func(_ context.Context, id string) (sessionsearch.SessionSearchManager, error) {
		return mgr, nil
	})
	memHits, err := transcript.Recall(context.Background(), memory.RecallQuery{
		Source:       memory.SourceTranscript,
		AgentID:      agentID,
		SessionID:    "other-sess",
		Query:        "phase1.conf",
		Limit:        5,
		AnchorWindow: 2,
		IncludeTools: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("SessionTranscript.Recall: %v", err)
	}
	if len(memHits) == 0 {
		t.Fatal("memory_recall transcript path returned no hits")
	}
	md := memHits[0].Metadata
	if md == nil {
		t.Fatalf("hit metadata nil: %+v", memHits[0])
	}
	anchor, _ := md["anchor"].(map[string]any)
	if anchor == nil {
		t.Fatalf("expected anchored metadata, hit=%+v", memHits[0])
	}
	if anchor["role"] != "tool" || anchor["tool_name"] != "execute_read" {
		t.Fatalf("anchor meta=%+v", anchor)
	}
	if !strings.Contains(memHits[0].Content, "execute_read") && !strings.Contains(memHits[0].Content, "tool=") {
		t.Fatalf("content should include tool projection, got %q", memHits[0].Content)
	}
}

func TestTrajectoryPhase1_TracePersistDisabledSkipsStore(t *testing.T) {
	t.Setenv("SATH_TRACE_PERSIST", "false")
	db, err := gorm.Open(sqlite.Open("file:phase1_disabled?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.TurnTraceRow{}); err != nil {
		t.Fatal(err)
	}
	store := data.NewTurnTraceStore(db)
	PersistTurnTraceIfEnabled(context.Background(), store, agent.TurnTraceMeta{
		SessionID: "s", AgentID: "a", RequestID: "r",
	}, &agent.RunTrace{
		RequestID: "r",
		ToolCalls: []agent.ToolCallRecord{{ToolName: "echo"}},
	}, nil)
	got, err := store.GetByRequest(context.Background(), "s", "r")
	if got != nil {
		t.Fatalf("expected nil when persist disabled, got %+v", got)
	}
	if err == nil {
		t.Fatal("expected not-found error when no row")
	}
}

func boolPtr(v bool) *bool { return &v }
