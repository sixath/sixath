package sessionsearch

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// schemaMetaKey marks the current on-disk schema version.
// Migration (v1→v2): ADD COLUMN messages.tool_name; FTS still indexes content only
// (tool name is expected in content via tool=…, or prepended at IndexMessage time).
const schemaMetaKey = "session_index_schema_v2"

const schemaMetaKeyLegacyV1 = "session_index_schema_v1"

// IndexManager SQLite FTS5 跨会话索引（每 agent 一个库文件）。
type IndexManager struct {
	agentID string
	db      *sql.DB
	mu      sync.Mutex
}

// OpenIndex 打开或创建 agent 级索引库。
func OpenIndex(storeDir, agentID string) (*IndexManager, error) {
	if agentID == "" {
		return nil, fmt.Errorf("sessionsearch: agent_id required")
	}
	if storeDir == "" {
		storeDir = "data/session_index"
	}
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(storeDir, agentID+".db")
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	m := &IndexManager{agentID: agentID, db: db}
	if err := m.ensureSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return m, nil
}

func (m *IndexManager) ensureSchema() error {
	_, err := m.db.Exec(`
		CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			title TEXT,
			parent_session_id TEXT,
			updated_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_sessions_agent_updated ON sessions(agent_id, updated_at DESC);
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			tool_name TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id);
		CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
			message_id UNINDEXED,
			session_id UNINDEXED,
			role UNINDEXED,
			content,
			tokenize='unicode61'
		);
	`)
	if err != nil {
		return err
	}
	if err := m.migrateToSchemaV2(); err != nil {
		return err
	}
	return nil
}

// migrateToSchemaV2 adds tool_name when upgrading a v1 DB (CREATE IF NOT EXISTS
// does not alter existing tables). FTS is left on content only — YAGNI.
func (m *IndexManager) migrateToSchemaV2() error {
	has, err := m.messagesHasColumn("tool_name")
	if err != nil {
		return err
	}
	if !has {
		if _, err := m.db.Exec(`ALTER TABLE messages ADD COLUMN tool_name TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("sessionsearch: migrate tool_name: %w", err)
		}
	}
	_, err = m.db.Exec(`
		INSERT INTO meta(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, schemaMetaKey, "2")
	if err != nil {
		return err
	}
	_, _ = m.db.Exec(`DELETE FROM meta WHERE key = ?`, schemaMetaKeyLegacyV1)
	return nil
}

func (m *IndexManager) messagesHasColumn(name string) (bool, error) {
	rows, err := m.db.Query(`PRAGMA table_info(messages)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var colName, colType string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &colName, &colType, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if colName == name {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (m *IndexManager) Close() error {
	if m == nil || m.db == nil {
		return nil
	}
	return m.db.Close()
}

// UpsertSession 写入/更新会话元数据。
func (m *IndexManager) UpsertSession(ctx context.Context, sess SessionMeta) error {
	if sess.ID == "" {
		return nil
	}
	agentID := sess.AgentID
	if agentID == "" {
		agentID = m.agentID
	}
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO sessions(id, agent_id, title, parent_session_id, updated_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			agent_id=excluded.agent_id,
			title=excluded.title,
			parent_session_id=excluded.parent_session_id,
			updated_at=excluded.updated_at
	`, sess.ID, agentID, sess.Title, nullIfEmpty(sess.ParentSessionID), sess.UpdatedAt.Unix())
	return err
}

// IndexMessage 增量索引单条消息。
func (m *IndexManager) IndexMessage(ctx context.Context, sess SessionMeta, msg MessageDoc) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.UpsertSession(ctx, sess); err != nil {
		return err
	}
	if msg.ID == "" || msg.SessionID == "" {
		return nil
	}
	content := msg.Content
	ftsContent := ftsContentForMessage(msg)
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO messages(id, session_id, role, content, tool_name, created_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			session_id=excluded.session_id,
			role=excluded.role,
			content=excluded.content,
			tool_name=excluded.tool_name,
			created_at=excluded.created_at
	`, msg.ID, msg.SessionID, msg.Role, content, msg.ToolName, msg.CreatedAt.Unix())
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `DELETE FROM messages_fts WHERE message_id = ?`, msg.ID)
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `
		INSERT INTO messages_fts(message_id, session_id, role, content)
		VALUES(?,?,?,?)
	`, msg.ID, msg.SessionID, msg.Role, ftsContent)
	return err
}

// ftsContentForMessage ensures ToolName is searchable when set but missing from Content.
func ftsContentForMessage(msg MessageDoc) string {
	if msg.ToolName == "" || strings.Contains(msg.Content, msg.ToolName) {
		return msg.Content
	}
	return "tool=" + msg.ToolName + " " + msg.Content
}

// RemoveSession 删除会话及其 FTS 行。
func (m *IndexManager) RemoveSession(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows, err := m.db.QueryContext(ctx, `SELECT id FROM messages WHERE session_id = ?`, sessionID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		_, _ = m.db.ExecContext(ctx, `DELETE FROM messages_fts WHERE message_id = ?`, id)
	}
	_, err = m.db.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ?`, sessionID)
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	return err
}

// RemoveMessages hard-deletes messages and FTS rows by id. No-op on empty.
func (m *IndexManager) RemoveMessages(ctx context.Context, messageIDs []string) error {
	if len(messageIDs) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range messageIDs {
		if id == "" {
			continue
		}
		if _, err := m.db.ExecContext(ctx, `DELETE FROM messages_fts WHERE message_id = ?`, id); err != nil {
			return err
		}
		if _, err := m.db.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// RemoveTraceProjections deletes FTS docs for a TurnTrace request (id prefix "trace:{requestID}:").
func (m *IndexManager) RemoveTraceProjections(ctx context.Context, sessionID, requestID string) error {
	if sessionID == "" || requestID == "" {
		return nil
	}
	prefix := "trace:" + requestID + ":"
	m.mu.Lock()
	defer m.mu.Unlock()
	rows, err := m.db.QueryContext(ctx, `
		SELECT id FROM messages WHERE session_id = ? AND id LIKE ?
	`, sessionID, prefix+"%")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if _, err := m.db.ExecContext(ctx, `DELETE FROM messages_fts WHERE message_id = ?`, id); err != nil {
			return err
		}
		if _, err := m.db.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

type ftsHit struct {
	SessionID string
	MessageID string
	Snippet   string
	Score     float64 // higher is better (-bm25)
}

func (m *IndexManager) searchFTS(ctx context.Context, matchExpr string, roleFilter []string, limit int) ([]ftsHit, error) {
	return m.searchFTSScored(ctx, matchExpr, roleFilter, limit)
}

func (m *IndexManager) searchFTSScored(ctx context.Context, matchExpr string, roleFilter []string, limit int) ([]ftsHit, error) {
	if matchExpr == "" {
		return nil, nil
	}
	q := `
		SELECT session_id, message_id,
			snippet(messages_fts, 3, '[', ']', '…', 32) AS snip,
			bm25(messages_fts) AS rank
		FROM messages_fts
		WHERE messages_fts MATCH ?
	`
	args := []any{matchExpr}
	if len(roleFilter) > 0 {
		placeholders := strings.Repeat("?,", len(roleFilter))
		placeholders = placeholders[:len(placeholders)-1]
		q += ` AND role IN (` + placeholders + `)`
		for _, r := range roleFilter {
			args = append(args, strings.TrimSpace(r))
		}
	}
	q += ` ORDER BY rank LIMIT ?`
	args = append(args, limit*8)
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []ftsHit
	for rows.Next() {
		var h ftsHit
		var rank float64
		if err := rows.Scan(&h.SessionID, &h.MessageID, &h.Snippet, &rank); err != nil {
			continue
		}
		h.Score = -rank // bm25: more negative is better
		hits = append(hits, h)
	}
	return hits, nil
}

func (m *IndexManager) loadSessions(ctx context.Context, agentID string) (map[string]SessionMeta, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, agent_id, title, parent_session_id, updated_at
		FROM sessions WHERE agent_id = ?
	`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]SessionMeta)
	for rows.Next() {
		var s SessionMeta
		var parent sql.NullString
		var updated int64
		if err := rows.Scan(&s.ID, &s.AgentID, &s.Title, &parent, &updated); err != nil {
			continue
		}
		if parent.Valid {
			s.ParentSessionID = parent.String
		}
		s.UpdatedAt = time.Unix(updated, 0)
		out[s.ID] = s
	}
	return out, nil
}

func rootSessionID(sessions map[string]SessionMeta, id string) string {
	seen := make(map[string]bool)
	for id != "" {
		if seen[id] {
			return id
		}
		seen[id] = true
		s, ok := sessions[id]
		if !ok || s.ParentSessionID == "" {
			return id
		}
		id = s.ParentSessionID
	}
	return id
}

func (m *IndexManager) collapseHits(hits []ftsHit, sessions map[string]SessionMeta, excludeSessionID string, limit int) []SessionHit {
	type agg struct {
		hit SessionHit
		n   int
	}
	byRoot := make(map[string]*agg)
	for _, h := range hits {
		if h.SessionID == excludeSessionID {
			continue
		}
		root := rootSessionID(sessions, h.SessionID)
		if root == excludeSessionID {
			continue
		}
		sess := sessions[h.SessionID]
		if sess.ID == "" {
			sess = sessions[root]
		}
		a, ok := byRoot[root]
		if !ok {
			a = &agg{hit: SessionHit{
				SessionID:     h.SessionID,
				RootSessionID: root,
				Title:         sess.Title,
				UpdatedAt:     sess.UpdatedAt,
			}}
			byRoot[root] = a
		}
		a.n++
		if len(a.hit.MatchedSnippets) < 3 && h.Snippet != "" {
			a.hit.MatchedSnippets = append(a.hit.MatchedSnippets, h.Snippet)
		}
	}
	out := make([]SessionHit, 0, len(byRoot))
	for _, a := range byRoot {
		if a.hit.Preview == "" && len(a.hit.MatchedSnippets) > 0 {
			a.hit.Preview = a.hit.MatchedSnippets[0]
		}
		out = append(out, a.hit)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Search FTS 关键词检索。
func (m *IndexManager) Search(ctx context.Context, opts SearchOpts) ([]SessionHit, error) {
	match := BuildFTSMatchExpr(opts.Query)
	if match == "" {
		return nil, nil
	}
	limit := clampSearchLimit(opts.Limit)
	hits, err := m.searchFTS(ctx, match, opts.RoleFilter, limit)
	if err != nil {
		return nil, err
	}
	sessions, err := m.loadSessions(ctx, opts.AgentID)
	if err != nil {
		return nil, err
	}
	return m.collapseHits(hits, sessions, opts.ExcludeSessionID, limit), nil
}

// SearchAnchored returns message-level FTS hits with window/bookend context.
// Unlike Search, it does not collapseHits to session roots; RootSessionID walks parent chain.
func (m *IndexManager) SearchAnchored(ctx context.Context, opts SearchOpts, anchor AnchorOpts) ([]AnchoredHit, error) {
	match := BuildFTSMatchExpr(opts.Query)
	if match == "" {
		return nil, nil
	}
	limit := clampSearchLimit(opts.Limit)
	window, bookend := normalizeAnchorOpts(anchor)
	roleFilter := opts.RoleFilter
	if len(roleFilter) == 0 {
		roleFilter = []string{"user", "assistant", "tool"}
	}

	hits, err := m.searchFTSScored(ctx, match, roleFilter, limit)
	if err != nil {
		return nil, err
	}
	sessions, err := m.loadSessions(ctx, opts.AgentID)
	if err != nil {
		return nil, err
	}

	// Best score per session_id (message-level discovery, no parent collapse).
	type picked struct {
		hit ftsHit
	}
	best := make(map[string]picked)
	order := make([]string, 0)
	for _, h := range hits {
		if h.SessionID == "" || h.SessionID == opts.ExcludeSessionID {
			continue
		}
		if _, ok := sessions[h.SessionID]; !ok && opts.AgentID != "" {
			continue
		}
		if prev, ok := best[h.SessionID]; ok {
			if h.Score > prev.hit.Score {
				best[h.SessionID] = picked{hit: h}
			}
			continue
		}
		best[h.SessionID] = picked{hit: h}
		order = append(order, h.SessionID)
	}

	out := make([]AnchoredHit, 0, limit)
	for _, sid := range order {
		if len(out) >= limit {
			break
		}
		p := best[sid]
		sess := sessions[sid]
		anchorDoc, err := m.loadMessage(ctx, p.hit.MessageID)
		if err != nil || anchorDoc.ID == "" {
			continue
		}
		win, err := m.messagesAround(ctx, sid, p.hit.MessageID, window)
		if err != nil {
			return nil, err
		}
		start, end, err := m.bookendMessages(ctx, sid, bookend)
		if err != nil {
			return nil, err
		}
		out = append(out, AnchoredHit{
			SessionID:     sid,
			RootSessionID: rootSessionID(sessions, sid),
			Title:         sess.Title,
			Anchor:        anchorDoc,
			Window:        win,
			BookendStart:  start,
			BookendEnd:    end,
			Score:         p.hit.Score,
		})
	}
	return out, nil
}

// GetMessagesAround returns ±window messages (by created_at) around messageID in the session.
func (m *IndexManager) GetMessagesAround(ctx context.Context, agentID, sessionID, messageID string, window int) ([]MessageDoc, error) {
	_ = agentID
	if window <= 0 {
		window = 5
	}
	return m.messagesAround(ctx, sessionID, messageID, window)
}

func clampSearchLimit(limit int) int {
	if limit <= 0 {
		return 3
	}
	if limit > 5 {
		return 5
	}
	return limit
}

func normalizeAnchorOpts(a AnchorOpts) (window, bookend int) {
	window = a.Window
	if window <= 0 {
		window = 5
	}
	bookend = a.Bookend
	if bookend <= 0 {
		bookend = 3
	}
	return window, bookend
}

func (m *IndexManager) loadMessage(ctx context.Context, messageID string) (MessageDoc, error) {
	var doc MessageDoc
	var created int64
	err := m.db.QueryRowContext(ctx, `
		SELECT id, session_id, role, content, tool_name, created_at
		FROM messages WHERE id = ?
	`, messageID).Scan(&doc.ID, &doc.SessionID, &doc.Role, &doc.Content, &doc.ToolName, &created)
	if err == sql.ErrNoRows {
		return MessageDoc{}, nil
	}
	if err != nil {
		return MessageDoc{}, err
	}
	doc.CreatedAt = time.Unix(created, 0)
	return doc, nil
}

func (m *IndexManager) listSessionMessages(ctx context.Context, sessionID string) ([]MessageDoc, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, session_id, role, content, tool_name, created_at
		FROM messages WHERE session_id = ?
		ORDER BY created_at ASC, id ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessageDocs(rows)
}

func scanMessageDocs(rows *sql.Rows) ([]MessageDoc, error) {
	var out []MessageDoc
	for rows.Next() {
		var doc MessageDoc
		var created int64
		if err := rows.Scan(&doc.ID, &doc.SessionID, &doc.Role, &doc.Content, &doc.ToolName, &created); err != nil {
			continue
		}
		doc.CreatedAt = time.Unix(created, 0)
		out = append(out, doc)
	}
	return out, rows.Err()
}

func (m *IndexManager) messagesAround(ctx context.Context, sessionID, messageID string, window int) ([]MessageDoc, error) {
	msgs, err := m.listSessionMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i, msg := range msgs {
		if msg.ID == messageID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, nil
	}
	start := idx - window
	if start < 0 {
		start = 0
	}
	end := idx + window + 1
	if end > len(msgs) {
		end = len(msgs)
	}
	out := make([]MessageDoc, end-start)
	copy(out, msgs[start:end])
	return out, nil
}

func (m *IndexManager) bookendMessages(ctx context.Context, sessionID string, n int) (start, end []MessageDoc, err error) {
	if n <= 0 {
		return nil, nil, nil
	}
	startRows, err := m.db.QueryContext(ctx, `
		SELECT id, session_id, role, content, tool_name, created_at
		FROM messages
		WHERE session_id = ? AND role IN ('user', 'assistant')
		ORDER BY created_at ASC, id ASC
		LIMIT ?
	`, sessionID, n)
	if err != nil {
		return nil, nil, err
	}
	start, err = scanMessageDocs(startRows)
	startRows.Close()
	if err != nil {
		return nil, nil, err
	}

	endRows, err := m.db.QueryContext(ctx, `
		SELECT id, session_id, role, content, tool_name, created_at
		FROM messages
		WHERE session_id = ? AND role IN ('user', 'assistant')
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, sessionID, n)
	if err != nil {
		return nil, nil, err
	}
	end, err = scanMessageDocs(endRows)
	endRows.Close()
	if err != nil {
		return nil, nil, err
	}
	// reverse to chronological order
	for i, j := 0, len(end)-1; i < j; i, j = i+1, j-1 {
		end[i], end[j] = end[j], end[i]
	}
	return start, end, nil
}

// ListRecent 最近会话（无 query，零 LLM 成本）。
func (m *IndexManager) ListRecent(ctx context.Context, agentID, excludeSessionID string, limit int) ([]SessionHit, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, title, updated_at FROM sessions
		WHERE agent_id = ? AND id <> ?
		ORDER BY updated_at DESC LIMIT ?
	`, agentID, excludeSessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionHit
	for rows.Next() {
		var h SessionHit
		var updated int64
		if err := rows.Scan(&h.SessionID, &h.Title, &updated); err != nil {
			continue
		}
		h.RootSessionID = h.SessionID
		h.UpdatedAt = time.Unix(updated, 0)
		preview, _ := m.lastMessagePreview(ctx, h.SessionID)
		h.Preview = preview
		if topic, err := m.lastUserMessagePreview(ctx, h.SessionID); err == nil && topic != "" {
			// Prefer latest user ask as display title when stored title is stale/mismatched.
			h.Title = topic
		}
		out = append(out, h)
	}
	return out, nil
}

func (m *IndexManager) lastMessagePreview(ctx context.Context, sessionID string) (string, error) {
	var content string
	err := m.db.QueryRowContext(ctx, `
		SELECT content FROM messages WHERE session_id = ?
		ORDER BY created_at DESC LIMIT 1
	`, sessionID).Scan(&content)
	if err != nil {
		return "", err
	}
	content = strings.TrimSpace(content)
	if len(content) > 200 {
		content = content[:200] + "…"
	}
	return content, nil
}

func (m *IndexManager) lastUserMessagePreview(ctx context.Context, sessionID string) (string, error) {
	var content string
	err := m.db.QueryRowContext(ctx, `
		SELECT content FROM messages WHERE session_id = ? AND role = 'user'
		ORDER BY created_at DESC LIMIT 1
	`, sessionID).Scan(&content)
	if err != nil {
		return "", err
	}
	content = strings.TrimSpace(strings.ReplaceAll(content, "\n", " "))
	if len([]rune(content)) > 40 {
		r := []rune(content)
		content = string(r[:40]) + "…"
	}
	return content, nil
}

// EnsureSynced 从 SyncSource 全量回补（幂等）。
func (m *IndexManager) EnsureSynced(ctx context.Context, agentID string, src SyncSource) error {
	if src == nil {
		return nil
	}
	sessions, err := src.ListSessions(ctx, agentID, 500)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if err := m.UpsertSession(ctx, sess); err != nil {
			return err
		}
		msgs, err := src.ListMessages(ctx, sess.ID, 1000)
		if err != nil {
			return err
		}
		for _, msg := range msgs {
			if err := m.IndexMessage(ctx, sess, msg); err != nil {
				return err
			}
		}
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
