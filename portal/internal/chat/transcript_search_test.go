package chat

import (
	"context"
	"testing"
	"time"

	"backend/internal/biz"

	"github.com/sixath/framework/sessionsearch"
)

type anchoredFakeManager struct {
	fakeSessionSearchManager
	hits           []sessionsearch.AnchoredHit
	lastOpts       sessionsearch.SearchOpts
	lastAnchor     sessionsearch.AnchorOpts
	searchAnchored int
	ensureSynced   int
}

func (f *anchoredFakeManager) SearchAnchored(_ context.Context, opts sessionsearch.SearchOpts, anchor sessionsearch.AnchorOpts) ([]sessionsearch.AnchoredHit, error) {
	f.searchAnchored++
	f.lastOpts = opts
	f.lastAnchor = anchor
	return f.hits, f.err
}

func (f *anchoredFakeManager) EnsureSynced(context.Context, string, sessionsearch.SyncSource) error {
	f.ensureSynced++
	return nil
}

func TestSessionSearchBackend_SearchAnchored_WithFakeManager(t *testing.T) {
	prev := DefaultSessionSearchConfig
	DefaultSessionSearchConfig.Enabled = true
	t.Cleanup(func() { DefaultSessionSearchConfig = prev })

	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	mgr := &anchoredFakeManager{
		hits: []sessionsearch.AnchoredHit{{
			SessionID:     "sess-1",
			RootSessionID: "sess-1",
			Title:         "Demo",
			Anchor: sessionsearch.MessageDoc{
				ID: "m1", SessionID: "sess-1", Role: "tool", Content: "db lookup", ToolName: "sql_query", CreatedAt: now,
			},
			Window: []sessionsearch.MessageDoc{
				{ID: "m0", SessionID: "sess-1", Role: "user", Content: "how?", CreatedAt: now},
			},
			BookendStart: []sessionsearch.MessageDoc{},
			BookendEnd:   []sessionsearch.MessageDoc{},
			Score:        1.5,
		}},
	}

	backend := NewSessionSearchBackendWithManager(nil, func(_ context.Context, agentID string) (sessionsearch.SessionSearchManager, error) {
		if agentID != "agent-1" {
			t.Fatalf("agentID=%q want agent-1", agentID)
		}
		return mgr, nil
	})

	hits, err := backend.SearchAnchored(context.Background(), biz.TranscriptSearchOpts{
		AgentID:          "agent-1",
		Query:            "sql_query",
		ExcludeSessionID: "current",
		IncludeTools:     true,
		Window:           5,
	})
	if err != nil {
		t.Fatalf("SearchAnchored: %v", err)
	}
	if mgr.searchAnchored != 1 {
		t.Fatalf("SearchAnchored calls=%d want 1", mgr.searchAnchored)
	}
	if mgr.lastOpts.ExcludeSessionID != "current" {
		t.Fatalf("ExcludeSessionID=%q want current", mgr.lastOpts.ExcludeSessionID)
	}
	if len(mgr.lastOpts.RoleFilter) != 0 {
		t.Fatalf("RoleFilter=%v want empty when include_tools", mgr.lastOpts.RoleFilter)
	}
	if mgr.lastAnchor.Window != 5 {
		t.Fatalf("Window=%d want 5", mgr.lastAnchor.Window)
	}
	if len(hits) != 1 || hits[0].Anchor.ToolName != "sql_query" || hits[0].Score != 1.5 {
		t.Fatalf("hits=%+v", hits)
	}
	if len(hits[0].Window) != 1 || hits[0].Window[0].ID != "m0" {
		t.Fatalf("window=%+v", hits[0].Window)
	}
}

func TestSessionSearchBackend_SearchAnchored_ExcludeTools(t *testing.T) {
	prev := DefaultSessionSearchConfig
	DefaultSessionSearchConfig.Enabled = true
	t.Cleanup(func() { DefaultSessionSearchConfig = prev })

	mgr := &anchoredFakeManager{}
	backend := NewSessionSearchBackendWithManager(nil, func(context.Context, string) (sessionsearch.SessionSearchManager, error) {
		return mgr, nil
	})

	_, err := backend.SearchAnchored(context.Background(), biz.TranscriptSearchOpts{
		AgentID:      "a1",
		Query:        "needle",
		IncludeTools: false,
		Window:       3,
	})
	if err != nil {
		t.Fatalf("SearchAnchored: %v", err)
	}
	want := []string{"user", "assistant"}
	if len(mgr.lastOpts.RoleFilter) != 2 || mgr.lastOpts.RoleFilter[0] != want[0] || mgr.lastOpts.RoleFilter[1] != want[1] {
		t.Fatalf("RoleFilter=%v want %v", mgr.lastOpts.RoleFilter, want)
	}
}

func TestSessionSearchBackend_SearchAnchored_Disabled(t *testing.T) {
	prev := DefaultSessionSearchConfig
	DefaultSessionSearchConfig.Enabled = false
	t.Cleanup(func() { DefaultSessionSearchConfig = prev })

	called := false
	backend := NewSessionSearchBackendWithManager(nil, func(context.Context, string) (sessionsearch.SessionSearchManager, error) {
		called = true
		return nil, nil
	})
	hits, err := backend.SearchAnchored(context.Background(), biz.TranscriptSearchOpts{AgentID: "a", Query: "q", IncludeTools: true})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if called {
		t.Fatal("getManager should not be called when disabled")
	}
	if hits != nil {
		t.Fatalf("hits=%v want nil", hits)
	}
}
