package sessionsearch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildFTSMatchExpr_OR(t *testing.T) {
	expr := BuildFTSMatchExpr("deploy kubernetes")
	if expr == "" || !strings.Contains(expr, "deploy") || !strings.Contains(expr, "OR") || !strings.Contains(expr, "kubernetes") {
		t.Fatalf("expr=%q", expr)
	}
}

func TestIndexManager_SearchAndRecent(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ctx := context.Background()
	now := time.Now()
	sess := SessionMeta{ID: "s1", AgentID: "agent-1", Title: "Deploy chat", UpdatedAt: now}
	msg := MessageDoc{ID: "m1", SessionID: "s1", Role: "user", Content: "how to deploy kubernetes", CreatedAt: now}
	if err := idx.IndexMessage(ctx, sess, msg); err != nil {
		t.Fatal(err)
	}
	hits, err := idx.Search(ctx, SearchOpts{AgentID: "agent-1", Query: "kubernetes deploy", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected search hits")
	}
	recent, err := idx.ListRecent(ctx, "agent-1", "", 5)
	if err != nil || len(recent) == 0 {
		t.Fatalf("recent err=%v len=%d", err, len(recent))
	}
}

func TestRootSessionID(t *testing.T) {
	sessions := map[string]SessionMeta{
		"c": {ID: "c", ParentSessionID: "b"},
		"b": {ID: "b", ParentSessionID: "a"},
		"a": {ID: "a"},
	}
	if rootSessionID(sessions, "c") != "a" {
		t.Fatal("expected root a")
	}
}

func TestOpenIndex_createsFile(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir, "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.db")); err != nil {
		t.Fatal(err)
	}
}
