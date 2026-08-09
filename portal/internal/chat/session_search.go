package chat

import (
	"context"
	"log"
	"os"

	"backend/internal/biz"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/sessionsearch"
)

// DefaultSessionSearchConfig R1 默认配置；main 可通过环境变量覆盖。
var DefaultSessionSearchConfig = config.SessionSearchConfig{
	Enabled:  true,
	StoreDir: "data/session_index",
}

type sessionSearchBackend struct {
	chatUC     *biz.ChatUsecase
	getManager func(ctx context.Context, agentID string) (sessionsearch.SessionSearchManager, error)
}

// NewSessionSearchBackend adapts the legacy FTS index for the business search
// endpoint without exposing sessionsearch outside this wiring package.
func NewSessionSearchBackend(chatUC *biz.ChatUsecase) biz.SessionSearchBackend {
	return &sessionSearchBackend{
		chatUC:     chatUC,
		getManager: defaultSessionSearchManager,
	}
}

// NewSessionSearchBackendWithManager is for tests that inject a fake SessionSearchManager.
func NewSessionSearchBackendWithManager(
	chatUC *biz.ChatUsecase,
	getManager func(ctx context.Context, agentID string) (sessionsearch.SessionSearchManager, error),
) biz.SessionSearchBackend {
	if getManager == nil {
		getManager = defaultSessionSearchManager
	}
	return &sessionSearchBackend{chatUC: chatUC, getManager: getManager}
}

func defaultSessionSearchManager(_ context.Context, agentID string) (sessionsearch.SessionSearchManager, error) {
	cfg := config.Config{SessionSearch: DefaultSessionSearchConfig}
	if !cfg.SessionSearch.Enabled {
		return nil, nil
	}
	return sessionsearch.GetSessionSearchManager(cfg, agentID)
}

func (b *sessionSearchBackend) SearchSessions(ctx context.Context, agentIDs []string, query string, limit int) []biz.SessionSearchCandidate {
	if b == nil || b.chatUC == nil || !DefaultSessionSearchConfig.Enabled {
		return nil
	}
	source := NewSessionSearchSyncProvider(b.chatUC)
	out := make([]biz.SessionSearchCandidate, 0)
	for _, agentID := range agentIDs {
		mgr, err := b.getManager(ctx, agentID)
		if err != nil || mgr == nil {
			continue
		}
		_ = mgr.EnsureSynced(ctx, agentID, source)
		hits, err := mgr.Search(ctx, sessionsearch.SearchOpts{
			AgentID:    agentID,
			Query:      query,
			RoleFilter: []string{"user", "assistant"},
			Limit:      limit,
		})
		if err != nil {
			continue
		}
		for _, hit := range hits {
			out = append(out, biz.SessionSearchCandidate{
				SessionID:       hit.SessionID,
				RootSessionID:   hit.RootSessionID,
				AgentID:         agentID,
				Title:           hit.Title,
				Preview:         hit.Preview,
				MatchedSnippets: hit.MatchedSnippets,
				UpdatedAt:       hit.UpdatedAt,
			})
		}
	}
	return out
}

func (b *sessionSearchBackend) SearchAnchored(ctx context.Context, opts biz.TranscriptSearchOpts) ([]biz.AnchoredHit, error) {
	if b == nil || !DefaultSessionSearchConfig.Enabled {
		return nil, nil
	}
	mgr, err := b.getManager(ctx, opts.AgentID)
	if err != nil {
		return nil, err
	}
	if mgr == nil {
		return nil, nil
	}
	if b.chatUC != nil {
		_ = mgr.EnsureSynced(ctx, opts.AgentID, NewSessionSearchSyncProvider(b.chatUC))
	}
	searchOpts := sessionsearch.SearchOpts{
		AgentID:          opts.AgentID,
		Query:            opts.Query,
		ExcludeSessionID: opts.ExcludeSessionID,
		Limit:            opts.Limit,
	}
	if !opts.IncludeTools {
		searchOpts.RoleFilter = []string{"user", "assistant"}
	}
	hits, err := mgr.SearchAnchored(ctx, searchOpts, sessionsearch.AnchorOpts{Window: opts.Window})
	if err != nil {
		return nil, err
	}
	out := make([]biz.AnchoredHit, len(hits))
	for i, h := range hits {
		out[i] = biz.AnchoredHit{
			SessionID:     h.SessionID,
			RootSessionID: h.RootSessionID,
			Title:         h.Title,
			Anchor:        toBizMessageDoc(h.Anchor),
			Window:        toBizMessageDocs(h.Window),
			BookendStart:  toBizMessageDocs(h.BookendStart),
			BookendEnd:    toBizMessageDocs(h.BookendEnd),
			Score:         h.Score,
		}
	}
	return out, nil
}

func toBizMessageDoc(m sessionsearch.MessageDoc) biz.TranscriptMessageDoc {
	return biz.TranscriptMessageDoc{
		ID:        m.ID,
		SessionID: m.SessionID,
		Role:      m.Role,
		Content:   m.Content,
		ToolName:  m.ToolName,
		CreatedAt: m.CreatedAt,
	}
}

func toBizMessageDocs(in []sessionsearch.MessageDoc) []biz.TranscriptMessageDoc {
	if len(in) == 0 {
		return []biz.TranscriptMessageDoc{}
	}
	out := make([]biz.TranscriptMessageDoc, len(in))
	for i, m := range in {
		out[i] = toBizMessageDoc(m)
	}
	return out
}

// SessionSearchSyncProvider 实现 sessionsearch.SyncSource。
type SessionSearchSyncProvider struct {
	chatUC *biz.ChatUsecase
}

// NewSessionSearchSyncProvider creates sync source from ChatUsecase.
func NewSessionSearchSyncProvider(chatUC *biz.ChatUsecase) *SessionSearchSyncProvider {
	return &SessionSearchSyncProvider{chatUC: chatUC}
}

func (p *SessionSearchSyncProvider) ListSessions(ctx context.Context, agentID string, limit int) ([]sessionsearch.SessionMeta, error) {
	sessions, _, err := p.chatUC.ListSessions(ctx, agentID, "", 1, int32(limit), false)
	if err != nil {
		return nil, err
	}
	out := make([]sessionsearch.SessionMeta, len(sessions))
	for i, s := range sessions {
		out[i] = sessionMetaFromBiz(s)
	}
	return out, nil
}

func (p *SessionSearchSyncProvider) ListMessages(ctx context.Context, sessionID string, limit int) ([]sessionsearch.MessageDoc, error) {
	msgs, err := p.chatUC.ListMessages(ctx, sessionID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]sessionsearch.MessageDoc, len(msgs))
	for i, m := range msgs {
		out[i] = sessionsearch.MessageDoc{
			ID:        m.ID,
			SessionID: m.SessionID,
			Role:      m.Role,
			Content:   m.Content,
			CreatedAt: m.CreatedAt,
		}
	}
	return out, nil
}

func sessionMetaFromBiz(s *biz.ChatSession) sessionsearch.SessionMeta {
	if s == nil {
		return sessionsearch.SessionMeta{}
	}
	return sessionsearch.SessionMeta{
		ID:              s.ID,
		AgentID:         s.AgentID,
		Title:           s.Title,
		ParentSessionID: s.ParentSessionID,
		UpdatedAt:       s.UpdatedAt,
	}
}

// NotifySessionMessageIndexed 消息落库后增量写入 FTS 索引（异步）。
// 必须从仍带 caller 的请求 ctx 调用：goroutine 内用 DetachCallerContext，
// 避免 Background 丢身份导致 GetSession 静默失败（ACL 后 transcript 索引停更）。
func NotifySessionMessageIndexed(ctx context.Context, chatUC *biz.ChatUsecase, sessionID string, msg *biz.ChatMessage) {
	if chatUC == nil || msg == nil || sessionID == "" {
		return
	}
	bg := biz.DetachCallerContext(ctx)
	go func() {
		sess, err := chatUC.GetSession(bg, sessionID)
		if err != nil {
			log.Printf("sessionsearch index skip get session: session_id=%s err=%v", sessionID, err)
			return
		}
		cfg := config.Config{SessionSearch: DefaultSessionSearchConfig}
		if !cfg.SessionSearch.Enabled {
			return
		}
		mgr, err := sessionsearch.GetSessionSearchManager(cfg, sess.AgentID)
		if err != nil || mgr == nil {
			if err != nil {
				log.Printf("sessionsearch index skip manager: session_id=%s agent_id=%s err=%v", sessionID, sess.AgentID, err)
			}
			return
		}
		if err := mgr.IndexMessage(bg, sessionMetaFromBiz(sess), sessionsearch.MessageDoc{
			ID:        msg.ID,
			SessionID: msg.SessionID,
			Role:      msg.Role,
			Content:   msg.Content,
			CreatedAt: msg.CreatedAt,
		}); err != nil {
			log.Printf("sessionsearch IndexMessage failed: session_id=%s msg_id=%s err=%v", sessionID, msg.ID, err)
		}
	}()
}

// InitSessionSearchFromEnv 由 main 调用：SATH_SESSION_SEARCH_ENABLED、SATH_SESSION_INDEX_DIR。
func InitSessionSearchFromEnv() {
	if v := os.Getenv("SATH_SESSION_SEARCH_ENABLED"); v == "0" || v == "false" {
		DefaultSessionSearchConfig.Enabled = false
	}
	if dir := os.Getenv("SATH_SESSION_INDEX_DIR"); dir != "" {
		DefaultSessionSearchConfig.StoreDir = dir
	}
}
