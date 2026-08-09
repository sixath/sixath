package memory

import (
	"context"
	"testing"
	"time"

	"github.com/sixath/framework/sessionsearch"
)

type fakeSessionSearchManager struct {
	anchored         []sessionsearch.AnchoredHit
	recent           []sessionsearch.SessionHit
	searchAnchoredN  int
	listRecentN      int
	lastSearchOpts   sessionsearch.SearchOpts
	lastAnchorOpts   sessionsearch.AnchorOpts
	lastExclude      string
	lastListLimit    int
}

func (f *fakeSessionSearchManager) IndexMessage(context.Context, sessionsearch.SessionMeta, sessionsearch.MessageDoc) error {
	return nil
}
func (f *fakeSessionSearchManager) RemoveSession(context.Context, string, string) error { return nil }
func (f *fakeSessionSearchManager) RemoveMessages(context.Context, []string) error      { return nil }
func (f *fakeSessionSearchManager) RemoveTraceProjections(context.Context, string, string) error {
	return nil
}
func (f *fakeSessionSearchManager) Search(context.Context, sessionsearch.SearchOpts) ([]sessionsearch.SessionHit, error) {
	return nil, nil
}
func (f *fakeSessionSearchManager) SearchAnchored(_ context.Context, opts sessionsearch.SearchOpts, anchor sessionsearch.AnchorOpts) ([]sessionsearch.AnchoredHit, error) {
	f.searchAnchoredN++
	f.lastSearchOpts = opts
	f.lastAnchorOpts = anchor
	return f.anchored, nil
}
func (f *fakeSessionSearchManager) GetMessagesAround(context.Context, string, string, string, int) ([]sessionsearch.MessageDoc, error) {
	return nil, nil
}
func (f *fakeSessionSearchManager) ListRecent(_ context.Context, _ string, excludeSessionID string, limit int) ([]sessionsearch.SessionHit, error) {
	f.listRecentN++
	f.lastExclude = excludeSessionID
	f.lastListLimit = limit
	return f.recent, nil
}
func (f *fakeSessionSearchManager) EnsureSynced(context.Context, string, sessionsearch.SyncSource) error {
	return nil
}

func TestSessionTranscriptEmptyQueryListRecent(t *testing.T) {
	updated := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	mgr := &fakeSessionSearchManager{
		recent: []sessionsearch.SessionHit{{
			SessionID: "s-other", RootSessionID: "s-other", Title: "Past chat",
			Preview: "hello", UpdatedAt: updated,
		}},
	}
	backend := &SessionTranscript{
		Search: func(context.Context, RecallQuery) ([]MemoryHit, error) {
			t.Fatal("Search must not be called for an empty query")
			return nil, nil
		},
		GetManager: func(context.Context, string) (sessionsearch.SessionSearchManager, error) {
			return mgr, nil
		},
	}

	hits, err := backend.Recall(context.Background(), RecallQuery{
		AgentID: "agent-1", SessionID: "current", Query: "  ", Limit: 4,
	})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if mgr.listRecentN != 1 {
		t.Fatalf("ListRecent calls = %d, want 1", mgr.listRecentN)
	}
	if mgr.lastExclude != "current" {
		t.Fatalf("exclude = %q, want current", mgr.lastExclude)
	}
	if mgr.lastListLimit != 4 {
		t.Fatalf("limit = %d, want 4", mgr.lastListLimit)
	}
	if len(hits) != 1 || hits[0].ID != "s-other" {
		t.Fatalf("hits = %#v", hits)
	}
	if hits[0].Metadata["list_recent"] != true || hits[0].Metadata["title"] != "Past chat" {
		t.Fatalf("metadata = %#v", hits[0].Metadata)
	}
}

func TestSessionTranscriptNonEmptyUsesSearchAnchored(t *testing.T) {
	mgr := &fakeSessionSearchManager{
		anchored: []sessionsearch.AnchoredHit{{
			SessionID: "s1", RootSessionID: "s1", Title: "T",
			Anchor: sessionsearch.MessageDoc{ID: "m1", SessionID: "s1", Role: "user", Content: "deploy redis"},
			Window: []sessionsearch.MessageDoc{{ID: "m1", Role: "user", Content: "deploy redis"}},
			Score:  1.5,
		}},
	}
	backend := &SessionTranscript{
		GetManager: func(context.Context, string) (sessionsearch.SessionSearchManager, error) {
			return mgr, nil
		},
	}

	hits, err := backend.Recall(context.Background(), RecallQuery{
		AgentID: "agent-1", SessionID: "current", Query: "redis", Limit: 3, AnchorWindow: 7,
	})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if mgr.searchAnchoredN != 1 {
		t.Fatalf("SearchAnchored calls = %d, want 1", mgr.searchAnchoredN)
	}
	if mgr.lastSearchOpts.ExcludeSessionID != "current" {
		t.Fatalf("ExcludeSessionID = %q, want current", mgr.lastSearchOpts.ExcludeSessionID)
	}
	if len(mgr.lastSearchOpts.RoleFilter) != 0 {
		t.Fatalf("RoleFilter = %#v, want empty (default include tools)", mgr.lastSearchOpts.RoleFilter)
	}
	if mgr.lastAnchorOpts.Window != 7 {
		t.Fatalf("AnchorWindow = %d, want 7", mgr.lastAnchorOpts.Window)
	}
	if len(hits) != 1 || hits[0].Metadata["anchored"] != true {
		t.Fatalf("hits = %#v", hits)
	}
	anchor, _ := hits[0].Metadata["anchor"].(map[string]any)
	if anchor["content"] != "deploy redis" {
		t.Fatalf("anchor = %#v", anchor)
	}
}

func TestSessionTranscriptIncludeToolsFalse(t *testing.T) {
	mgr := &fakeSessionSearchManager{}
	backend := &SessionTranscript{
		GetManager: func(context.Context, string) (sessionsearch.SessionSearchManager, error) {
			return mgr, nil
		},
	}
	includeTools := false
	_, err := backend.Recall(context.Background(), RecallQuery{
		Query: "x", IncludeTools: &includeTools,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"user", "assistant"}
	if len(mgr.lastSearchOpts.RoleFilter) != 2 ||
		mgr.lastSearchOpts.RoleFilter[0] != want[0] ||
		mgr.lastSearchOpts.RoleFilter[1] != want[1] {
		t.Fatalf("RoleFilter = %#v, want %v", mgr.lastSearchOpts.RoleFilter, want)
	}
}

func TestSessionTranscriptExcludeCurrentFalse(t *testing.T) {
	mgr := &fakeSessionSearchManager{recent: []sessionsearch.SessionHit{}}
	backend := &SessionTranscript{
		GetManager: func(context.Context, string) (sessionsearch.SessionSearchManager, error) {
			return mgr, nil
		},
	}
	exclude := false
	_, err := backend.Recall(context.Background(), RecallQuery{
		SessionID: "current", Query: "", ExcludeCurrent: &exclude,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mgr.lastExclude != "" {
		t.Fatalf("exclude = %q, want empty", mgr.lastExclude)
	}

	_, err = backend.Recall(context.Background(), RecallQuery{
		SessionID: "current", Query: "q", ExcludeCurrent: &exclude,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mgr.lastSearchOpts.ExcludeSessionID != "" {
		t.Fatalf("ExcludeSessionID = %q, want empty", mgr.lastSearchOpts.ExcludeSessionID)
	}
}

func TestSessionTranscriptDelegatesNonEmptyQuery(t *testing.T) {
	want := []MemoryHit{{Scope: ScopeSession, Source: SourceTranscript, ID: "session-1", Content: "matched transcript"}}
	var got RecallQuery
	backend := &SessionTranscript{
		Search: func(_ context.Context, q RecallQuery) ([]MemoryHit, error) {
			got = q
			return want, nil
		},
	}

	hits, err := backend.Recall(context.Background(), RecallQuery{
		Scope:     ScopeSession,
		AgentID:   "agent-1",
		SessionID: "current",
		Query:     "deployment",
		Limit:     3,
	})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(hits) != 1 || hits[0].ID != want[0].ID {
		t.Fatalf("Recall() hits = %#v, want %#v", hits, want)
	}
	if got.Query != "deployment" || got.AgentID != "agent-1" || got.SessionID != "current" {
		t.Fatalf("Search() query = %#v, want forwarded query", got)
	}
}
