package sessionsearch

import (
	"context"
	"fmt"
	"sync"

	"github.com/sixath/framework/config"
)

var (
	mgrMu    sync.Mutex
	mgrCache = make(map[string]*IndexManager)
)

// ResolvedConfig 解析后的 R1 配置。
type ResolvedConfig struct {
	Enabled  bool
	StoreDir string
}

// ResolveConfig 从 framework Config 解析 session search 设置。
func ResolveConfig(cfg config.Config) ResolvedConfig {
	sc := cfg.SessionSearch
	out := ResolvedConfig{
		Enabled:  sc.Enabled,
		StoreDir: sc.StoreDir,
	}
	if out.StoreDir == "" {
		out.StoreDir = "data/session_index"
	}
	return out
}

// GetManager 按 agent 返回缓存的 IndexManager。
func GetManager(cfg config.Config, agentID string) (*IndexManager, error) {
	rc := ResolveConfig(cfg)
	if !rc.Enabled || agentID == "" {
		return nil, nil
	}
	mgrMu.Lock()
	defer mgrMu.Unlock()
	if m, ok := mgrCache[agentID]; ok {
		return m, nil
	}
	m, err := OpenIndex(rc.StoreDir, agentID)
	if err != nil {
		return nil, err
	}
	mgrCache[agentID] = m
	return m, nil
}

// service 将 IndexManager 适配为 SessionSearchManager。
type service struct {
	idx *IndexManager
}

func (s *service) IndexMessage(ctx context.Context, sess SessionMeta, msg MessageDoc) error {
	if s == nil || s.idx == nil {
		return nil
	}
	return s.idx.IndexMessage(ctx, sess, msg)
}

func (s *service) RemoveSession(ctx context.Context, agentID, sessionID string) error {
	if s == nil || s.idx == nil {
		return nil
	}
	_ = agentID
	return s.idx.RemoveSession(ctx, sessionID)
}

func (s *service) RemoveMessages(ctx context.Context, messageIDs []string) error {
	if s == nil || s.idx == nil {
		return nil
	}
	return s.idx.RemoveMessages(ctx, messageIDs)
}

func (s *service) RemoveTraceProjections(ctx context.Context, sessionID, requestID string) error {
	if s == nil || s.idx == nil {
		return nil
	}
	return s.idx.RemoveTraceProjections(ctx, sessionID, requestID)
}

func (s *service) Search(ctx context.Context, opts SearchOpts) ([]SessionHit, error) {
	if s == nil || s.idx == nil {
		return nil, fmt.Errorf("sessionsearch: disabled")
	}
	return s.idx.Search(ctx, opts)
}

func (s *service) SearchAnchored(ctx context.Context, opts SearchOpts, anchor AnchorOpts) ([]AnchoredHit, error) {
	if s == nil || s.idx == nil {
		return nil, fmt.Errorf("sessionsearch: disabled")
	}
	return s.idx.SearchAnchored(ctx, opts, anchor)
}

func (s *service) GetMessagesAround(ctx context.Context, agentID, sessionID, messageID string, window int) ([]MessageDoc, error) {
	if s == nil || s.idx == nil {
		return nil, fmt.Errorf("sessionsearch: disabled")
	}
	return s.idx.GetMessagesAround(ctx, agentID, sessionID, messageID, window)
}

func (s *service) ListRecent(ctx context.Context, agentID, excludeSessionID string, limit int) ([]SessionHit, error) {
	if s == nil || s.idx == nil {
		return nil, fmt.Errorf("sessionsearch: disabled")
	}
	return s.idx.ListRecent(ctx, agentID, excludeSessionID, limit)
}

func (s *service) EnsureSynced(ctx context.Context, agentID string, src SyncSource) error {
	if s == nil || s.idx == nil {
		return nil
	}
	return s.idx.EnsureSynced(ctx, agentID, src)
}

// GetSessionSearchManager 返回 SessionSearchManager；未启用时 nil,nil。
func GetSessionSearchManager(cfg config.Config, agentID string) (SessionSearchManager, error) {
	idx, err := GetManager(cfg, agentID)
	if err != nil || idx == nil {
		return nil, err
	}
	return &service{idx: idx}, nil
}

// ResetManagerCacheForTest closes and clears the per-agent IndexManager cache.
// Intended for tests that use temporary StoreDir paths (esp. on Windows).
func ResetManagerCacheForTest() {
	mgrMu.Lock()
	defer mgrMu.Unlock()
	for id, m := range mgrCache {
		if m != nil {
			_ = m.Close()
		}
		delete(mgrCache, id)
	}
}
