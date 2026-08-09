package memory

import (
	"context"
	"strings"

	"github.com/sixath/framework/sessionsearch"
)

// SessionTranscript adapts cross-session transcript search to TranscriptBackend.
// Search can be injected directly by callers that already own transcript retrieval
// (non-empty query only). Empty queries always use ListRecent via GetManager
// (or the optional ListRecent hook).
type SessionTranscript struct {
	Search     func(ctx context.Context, q RecallQuery) ([]MemoryHit, error)
	ListRecent func(ctx context.Context, q RecallQuery) ([]MemoryHit, error)
	GetManager func(ctx context.Context, agentID string) (sessionsearch.SessionSearchManager, error)
}

var _ TranscriptBackend = (*SessionTranscript)(nil)

func NewSessionTranscript(getManager func(context.Context, string) (sessionsearch.SessionSearchManager, error)) *SessionTranscript {
	return &SessionTranscript{GetManager: getManager}
}

func (s *SessionTranscript) Recall(ctx context.Context, q RecallQuery) ([]MemoryHit, error) {
	if strings.TrimSpace(q.Query) == "" {
		return s.listRecentAsHits(ctx, q)
	}
	if s != nil && s.Search != nil {
		return s.Search(ctx, q)
	}
	if s == nil || s.GetManager == nil {
		return []MemoryHit{}, nil
	}
	mgr, err := s.GetManager(ctx, q.AgentID)
	if err != nil {
		return nil, err
	}
	if mgr == nil {
		return []MemoryHit{}, nil
	}

	opts := sessionsearch.SearchOpts{
		AgentID: q.AgentID,
		Query:   q.Query,
		Limit:   q.Limit,
	}
	if q.ExcludeCurrentOrDefault() {
		opts.ExcludeSessionID = q.SessionID
	}
	if !q.IncludeToolsOrDefault() {
		opts.RoleFilter = []string{"user", "assistant"}
	}

	results, err := mgr.SearchAnchored(ctx, opts, sessionsearch.AnchorOpts{
		Window: q.AnchorWindow,
	})
	if err != nil {
		return nil, err
	}
	hits := make([]MemoryHit, 0, len(results))
	for _, result := range results {
		hits = append(hits, anchoredHitToMemoryHit(result))
	}
	return hits, nil
}

func (s *SessionTranscript) listRecentAsHits(ctx context.Context, q RecallQuery) ([]MemoryHit, error) {
	if s != nil && s.ListRecent != nil {
		return s.ListRecent(ctx, q)
	}
	if s == nil || s.GetManager == nil {
		return []MemoryHit{}, nil
	}
	mgr, err := s.GetManager(ctx, q.AgentID)
	if err != nil {
		return nil, err
	}
	if mgr == nil {
		return []MemoryHit{}, nil
	}
	exclude := ""
	if q.ExcludeCurrentOrDefault() {
		exclude = q.SessionID
	}
	results, err := mgr.ListRecent(ctx, q.AgentID, exclude, q.Limit)
	if err != nil {
		return nil, err
	}
	hits := make([]MemoryHit, 0, len(results))
	for _, result := range results {
		hits = append(hits, sessionHitToMemoryHit(result))
	}
	return hits, nil
}

func sessionHitToMemoryHit(result sessionsearch.SessionHit) MemoryHit {
	id := result.RootSessionID
	if id == "" {
		id = result.SessionID
	}
	content := result.Preview
	if len(result.MatchedSnippets) > 0 {
		content = strings.Join(result.MatchedSnippets, "\n")
	}
	return MemoryHit{
		Scope:   ScopeSession,
		Source:  SourceTranscript,
		ID:      id,
		Content: content,
		Metadata: map[string]any{
			"session_id":      result.SessionID,
			"root_session_id": result.RootSessionID,
			"title":           result.Title,
			"updated_at":      result.UpdatedAt,
			"list_recent":     true,
		},
	}
}

func anchoredHitToMemoryHit(result sessionsearch.AnchoredHit) MemoryHit {
	id := result.RootSessionID
	if id == "" {
		id = result.SessionID
	}
	return MemoryHit{
		Scope:   ScopeSession,
		Source:  SourceTranscript,
		ID:      id,
		Content: result.Anchor.Content,
		Score:   result.Score,
		Metadata: map[string]any{
			"session_id":      result.SessionID,
			"root_session_id": result.RootSessionID,
			"title":           result.Title,
			"anchor":          messageDocToMap(result.Anchor),
			"window":          messageDocsToMaps(result.Window),
			"bookend_start":   messageDocsToMaps(result.BookendStart),
			"bookend_end":     messageDocsToMaps(result.BookendEnd),
			"anchored":        true,
		},
	}
}

func messageDocToMap(m sessionsearch.MessageDoc) map[string]any {
	out := map[string]any{
		"id":         m.ID,
		"session_id": m.SessionID,
		"role":       m.Role,
		"content":    m.Content,
		"created_at": m.CreatedAt,
	}
	if m.ToolName != "" {
		out["tool_name"] = m.ToolName
	}
	return out
}

func messageDocsToMaps(docs []sessionsearch.MessageDoc) []map[string]any {
	out := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		out = append(out, messageDocToMap(d))
	}
	return out
}
