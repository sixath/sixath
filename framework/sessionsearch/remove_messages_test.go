package sessionsearch

import (
	"context"
	"testing"
	"time"
)

func TestRemoveMessages_DropsFTSHit(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir, "agent-rm")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ctx := context.Background()
	base := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	sess := SessionMeta{ID: "s1", AgentID: "agent-rm", Title: "t", UpdatedAt: base}

	for _, msg := range []MessageDoc{
		{ID: "m0", SessionID: "s1", Role: "user", Content: "keep uniquealpha", CreatedAt: base},
		{ID: "m1", SessionID: "s1", Role: "assistant", Content: "drop uniquebeta", CreatedAt: base.Add(time.Second)},
		{ID: "m2", SessionID: "s1", Role: "user", Content: "keep uniquegamma", CreatedAt: base.Add(2 * time.Second)},
	} {
		if err := idx.IndexMessage(ctx, sess, msg); err != nil {
			t.Fatal(err)
		}
	}
	if err := idx.RemoveMessages(ctx, []string{"m1"}); err != nil {
		t.Fatal(err)
	}

	hits, err := idx.Search(ctx, SearchOpts{AgentID: "agent-rm", Query: "uniquebeta", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits for removed content, got %d", len(hits))
	}
	keep, err := idx.Search(ctx, SearchOpts{AgentID: "agent-rm", Query: "uniquealpha", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(keep) == 0 {
		t.Fatal("expected keep hit for uniquealpha")
	}
}

func TestRemoveTraceProjections(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir, "agent-tr")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ctx := context.Background()
	base := time.Now().UTC()
	sess := SessionMeta{ID: "s1", AgentID: "agent-tr", Title: "t", UpdatedAt: base}
	doc := MessageDoc{
		ID: "trace:req99:c1", SessionID: "s1", Role: "tool", ToolName: "terminal",
		Content: "tool=terminal args={\"command\":\"echo uniqtrace99\"} result=ok", CreatedAt: base,
	}
	if err := idx.IndexMessage(ctx, sess, doc); err != nil {
		t.Fatal(err)
	}
	if err := idx.RemoveTraceProjections(ctx, "s1", "req99"); err != nil {
		t.Fatal(err)
	}
	hits, err := idx.SearchAnchored(ctx, SearchOpts{AgentID: "agent-tr", Query: "uniqtrace99", Limit: 3}, AnchorOpts{Window: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("want 0 after RemoveTraceProjections, got %d", len(hits))
	}
}

func TestSearchAnchored_RootSessionIDWalksParent(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir, "agent-fold")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ctx := context.Background()
	base := time.Date(2026, 8, 6, 14, 0, 0, 0, time.UTC)

	parent := SessionMeta{ID: "parent", AgentID: "agent-fold", Title: "root", UpdatedAt: base}
	child := SessionMeta{ID: "child", AgentID: "agent-fold", Title: "fork", ParentSessionID: "parent", UpdatedAt: base.Add(time.Minute)}
	if err := idx.UpsertSession(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if err := idx.IndexMessage(ctx, child, MessageDoc{
		ID: "c-m0", SessionID: "child", Role: "user", Content: "foldmarkerxyz", CreatedAt: base,
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := idx.SearchAnchored(ctx, SearchOpts{AgentID: "agent-fold", Query: "foldmarkerxyz", Limit: 3}, AnchorOpts{Window: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits=%d", len(hits))
	}
	if hits[0].SessionID != "child" {
		t.Fatalf("SessionID=%q", hits[0].SessionID)
	}
	if hits[0].RootSessionID != "parent" {
		t.Fatalf("RootSessionID=%q want parent", hits[0].RootSessionID)
	}
}
