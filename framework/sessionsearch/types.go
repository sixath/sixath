package sessionsearch

import (
	"context"
	"time"
)

// SessionMeta 会话元数据（跨会话检索域）。
type SessionMeta struct {
	ID              string
	AgentID         string
	Title           string
	ParentSessionID string
	UpdatedAt       time.Time
}

// MessageDoc 单条可索引消息。
type MessageDoc struct {
	ID        string
	SessionID string
	Role      string
	Content   string
	ToolName  string // optional; stored in messages.tool_name (schema v2)
	CreatedAt time.Time
}

// SearchOpts 检索参数。
type SearchOpts struct {
	AgentID          string
	Query            string
	ExcludeSessionID string
	RoleFilter       []string // 如 user,assistant
	Limit            int      // 默认 3，最大 5
}

// AnchorOpts controls window/bookend expansion for SearchAnchored.
type AnchorOpts struct {
	Window  int // ±N messages by created_at; default 5 if <=0
	Bookend int // first/last N user+assistant; default 3 if <=0
}

// AnchoredHit is a message-level FTS hit with local context (no session collapse).
type AnchoredHit struct {
	SessionID     string
	RootSessionID string
	Title         string
	Anchor        MessageDoc
	Window        []MessageDoc
	BookendStart  []MessageDoc
	BookendEnd    []MessageDoc
	Score         float64
}

// SessionHit 单条会话命中（已按 parent 链折叠到根会话）。
type SessionHit struct {
	SessionID       string
	RootSessionID   string
	Title           string
	UpdatedAt       time.Time
	Preview         string
	MatchedSnippets []string
}

// SessionSearchManager 跨会话 FTS 检索（R1）。
type SessionSearchManager interface {
	IndexMessage(ctx context.Context, sess SessionMeta, msg MessageDoc) error
	RemoveSession(ctx context.Context, agentID, sessionID string) error
	// RemoveMessages hard-deletes FTS + messages rows by id (Rewind).
	RemoveMessages(ctx context.Context, messageIDs []string) error
	// RemoveTraceProjections deletes docs with id LIKE "trace:{requestID}:%" in session.
	RemoveTraceProjections(ctx context.Context, sessionID, requestID string) error
	Search(ctx context.Context, opts SearchOpts) ([]SessionHit, error)
	SearchAnchored(ctx context.Context, opts SearchOpts, anchor AnchorOpts) ([]AnchoredHit, error)
	GetMessagesAround(ctx context.Context, agentID, sessionID, messageID string, window int) ([]MessageDoc, error)
	ListRecent(ctx context.Context, agentID, excludeSessionID string, limit int) ([]SessionHit, error)
	EnsureSynced(ctx context.Context, agentID string, src SyncSource) error
}

// SyncSource 从 Portal/MySQL 回补索引。
type SyncSource interface {
	ListSessions(ctx context.Context, agentID string, limit int) ([]SessionMeta, error)
	ListMessages(ctx context.Context, sessionID string, limit int) ([]MessageDoc, error)
}
