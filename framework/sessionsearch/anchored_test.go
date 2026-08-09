package sessionsearch

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSearchAnchored_ToolProjection(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir, "agent-anchored")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ctx := context.Background()
	base := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	sess := SessionMeta{ID: "s-tool", AgentID: "agent-anchored", Title: "Tool chat", UpdatedAt: base}

	msgs := []MessageDoc{
		{ID: "m0", SessionID: "s-tool", Role: "user", Content: "please inspect the config file", CreatedAt: base},
		{ID: "m1", SessionID: "s-tool", Role: "assistant", Content: "calling tool", CreatedAt: base.Add(time.Second)},
		{
			ID: "trace:r1:c1", SessionID: "s-tool", Role: "tool", ToolName: "execute_read",
			Content: "tool=execute_read err= args={\"path\":\"/tmp/a\"} result=ok",
			CreatedAt: base.Add(2 * time.Second),
		},
		{ID: "m3", SessionID: "s-tool", Role: "assistant", Content: "done reading", CreatedAt: base.Add(3 * time.Second)},
		{ID: "m4", SessionID: "s-tool", Role: "user", Content: "thanks", CreatedAt: base.Add(4 * time.Second)},
	}
	for _, msg := range msgs {
		if err := idx.IndexMessage(ctx, sess, msg); err != nil {
			t.Fatal(err)
		}
	}

	hits, err := idx.SearchAnchored(ctx, SearchOpts{
		AgentID: "agent-anchored",
		Query:   "execute_read",
		Limit:   3,
	}, AnchorOpts{Window: 2, Bookend: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 anchored hit, got %d", len(hits))
	}
	h := hits[0]
	if h.SessionID != "s-tool" || h.RootSessionID != "s-tool" {
		t.Fatalf("session ids: %+v", h)
	}
	if h.Title != "Tool chat" {
		t.Fatalf("title=%q", h.Title)
	}
	if h.Anchor.ID != "trace:r1:c1" || h.Anchor.Role != "tool" || h.Anchor.ToolName != "execute_read" {
		t.Fatalf("anchor=%+v", h.Anchor)
	}
	if len(h.Window) == 0 {
		t.Fatal("expected non-empty window")
	}
	windowIDs := map[string]bool{}
	for _, m := range h.Window {
		windowIDs[m.ID] = true
	}
	for _, want := range []string{"m1", "trace:r1:c1", "m3"} {
		if !windowIDs[want] {
			t.Fatalf("window missing %s: %+v", want, h.Window)
		}
	}
	if len(h.BookendStart) == 0 || len(h.BookendEnd) == 0 {
		t.Fatalf("bookends empty start=%d end=%d", len(h.BookendStart), len(h.BookendEnd))
	}
	for _, m := range append(h.BookendStart, h.BookendEnd...) {
		if m.Role == "tool" {
			t.Fatalf("bookend must be user/assistant only, got %+v", m)
		}
	}
}

func TestSearchAnchored_NoCollapseMultipleSessions(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir, "agent-multi")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ctx := context.Background()
	base := time.Date(2026, 8, 5, 13, 0, 0, 0, time.UTC)

	for i, sid := range []string{"s-a", "s-b"} {
		sess := SessionMeta{
			ID: sid, AgentID: "agent-multi", Title: "session " + sid,
			UpdatedAt: base.Add(time.Duration(i) * time.Minute),
		}
		msgs := []MessageDoc{
			{ID: sid + "-u", SessionID: sid, Role: "user", Content: "kubernetes rollout", CreatedAt: base},
			{ID: sid + "-a", SessionID: sid, Role: "assistant", Content: "planning kubernetes deploy", CreatedAt: base.Add(time.Second)},
		}
		for _, msg := range msgs {
			if err := idx.IndexMessage(ctx, sess, msg); err != nil {
				t.Fatal(err)
			}
		}
	}

	hits, err := idx.SearchAnchored(ctx, SearchOpts{
		AgentID: "agent-multi",
		Query:   "kubernetes",
		Limit:   5,
	}, AnchorOpts{Window: 1, Bookend: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 message-level session hits (no collapse), got %d", len(hits))
	}
	seen := map[string]bool{}
	for _, h := range hits {
		if seen[h.SessionID] {
			t.Fatalf("duplicate session %s", h.SessionID)
		}
		seen[h.SessionID] = true
		if h.RootSessionID != h.SessionID {
			t.Fatalf("phase1 RootSessionID must equal SessionID: %+v", h)
		}
	}
}

func TestSearchAnchored_RoleFilterExcludeTools(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir, "agent-roles")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ctx := context.Background()
	base := time.Now()
	sess := SessionMeta{ID: "s1", AgentID: "agent-roles", Title: "roles", UpdatedAt: base}
	docs := []MessageDoc{
		{ID: "u1", SessionID: "s1", Role: "user", Content: "please check the cluster status", CreatedAt: base},
		{
			ID: "t1", SessionID: "s1", Role: "tool", ToolName: "kubectl",
			Content: "tool=kubectl err= args=get pods result=Running",
			CreatedAt: base.Add(time.Second),
		},
		{ID: "u2", SessionID: "s1", Role: "user", Content: "looks good", CreatedAt: base.Add(2 * time.Second)},
	}
	for _, d := range docs {
		if err := idx.IndexMessage(ctx, sess, d); err != nil {
			t.Fatal(err)
		}
	}

	withTools, err := idx.SearchAnchored(ctx, SearchOpts{
		AgentID: "agent-roles", Query: "kubectl", Limit: 3,
	}, AnchorOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(withTools) != 1 || withTools[0].Anchor.Role != "tool" {
		t.Fatalf("default role filter should include tools: %+v", withTools)
	}

	// Index a user message that also mentions the tool name; exclude tool roles.
	if err := idx.IndexMessage(ctx, sess, MessageDoc{
		ID: "u3", SessionID: "s1", Role: "user", Content: "docs about kubectl usage",
		CreatedAt: base.Add(3 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	noTools, err := idx.SearchAnchored(ctx, SearchOpts{
		AgentID: "agent-roles", Query: "kubectl", Limit: 3,
		RoleFilter: []string{"user", "assistant"},
	}, AnchorOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(noTools) != 1 || noTools[0].Anchor.Role != "user" {
		t.Fatalf("RoleFilter without tool should hit user: %+v", noTools)
	}
}

func TestSearchAnchored_BestScorePerSession(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir, "agent-dedupe")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ctx := context.Background()
	base := time.Now()
	sess := SessionMeta{ID: "s1", AgentID: "agent-dedupe", Title: "dedupe", UpdatedAt: base}
	for i, content := range []string{
		"deploy kubernetes cluster alpha",
		"kubernetes notes",
		"deploy kubernetes cluster beta more kubernetes kubernetes",
	} {
		msg := MessageDoc{
			ID: fmt.Sprintf("m%d", i), SessionID: "s1", Role: "user",
			Content: content, CreatedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := idx.IndexMessage(ctx, sess, msg); err != nil {
			t.Fatal(err)
		}
	}

	hits, err := idx.SearchAnchored(ctx, SearchOpts{
		AgentID: "agent-dedupe", Query: "kubernetes", Limit: 5,
	}, AnchorOpts{Window: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit after per-session dedupe, got %d", len(hits))
	}
}

func TestGetMessagesAround(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir, "agent-around")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ctx := context.Background()
	base := time.Now()
	sess := SessionMeta{ID: "s1", AgentID: "agent-around", Title: "around", UpdatedAt: base}
	for i := 0; i < 7; i++ {
		msg := MessageDoc{
			ID: fmt.Sprintf("m%d", i), SessionID: "s1", Role: "user",
			Content: fmt.Sprintf("msg %d", i), CreatedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := idx.IndexMessage(ctx, sess, msg); err != nil {
			t.Fatal(err)
		}
	}

	around, err := idx.GetMessagesAround(ctx, "agent-around", "s1", "m3", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(around) != 5 {
		t.Fatalf("expected 5 messages (±2), got %d %+v", len(around), around)
	}
	if around[0].ID != "m1" || around[2].ID != "m3" || around[4].ID != "m5" {
		t.Fatalf("unexpected order: %+v", around)
	}
}

func TestSchemaV2_ToolNameColumn(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir, "agent-schema")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ctx := context.Background()
	sess := SessionMeta{ID: "s1", AgentID: "agent-schema", Title: "t", UpdatedAt: time.Now()}
	msg := MessageDoc{
		ID: "t1", SessionID: "s1", Role: "tool", ToolName: "http_get",
		Content: "tool=http_get result=200", CreatedAt: time.Now(),
	}
	if err := idx.IndexMessage(ctx, sess, msg); err != nil {
		t.Fatal(err)
	}
	var toolName string
	err = idx.db.QueryRow(`SELECT tool_name FROM messages WHERE id = ?`, "t1").Scan(&toolName)
	if err != nil {
		t.Fatal(err)
	}
	if toolName != "http_get" {
		t.Fatalf("tool_name=%q", toolName)
	}
	var ver string
	err = idx.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, schemaMetaKey).Scan(&ver)
	if err != nil {
		t.Fatal(err)
	}
	if ver != "2" {
		t.Fatalf("schema version=%q want 2", ver)
	}
}

func TestSchemaV2_MigratesLegacyDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-legacy.db")
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY, agent_id TEXT NOT NULL, title TEXT,
			parent_session_id TEXT, updated_at INTEGER NOT NULL
		);
		CREATE TABLE messages (
			id TEXT PRIMARY KEY, session_id TEXT NOT NULL, role TEXT NOT NULL,
			content TEXT NOT NULL, created_at INTEGER NOT NULL
		);
		CREATE VIRTUAL TABLE messages_fts USING fts5(
			message_id UNINDEXED, session_id UNINDEXED, role UNINDEXED, content,
			tokenize='unicode61'
		);
		INSERT INTO meta(key,value) VALUES('session_index_schema_v1','1');
	`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	idx, err := OpenIndex(dir, "agent-legacy")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	has, err := idx.messagesHasColumn("tool_name")
	if err != nil || !has {
		t.Fatalf("tool_name column missing after migrate: has=%v err=%v", has, err)
	}
	var ver string
	if err := idx.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, schemaMetaKey).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != "2" {
		t.Fatalf("ver=%q", ver)
	}
	var legacy int
	_ = idx.db.QueryRow(`SELECT COUNT(1) FROM meta WHERE key = ?`, schemaMetaKeyLegacyV1).Scan(&legacy)
	if legacy != 0 {
		t.Fatalf("legacy meta key should be removed, count=%d", legacy)
	}
}
