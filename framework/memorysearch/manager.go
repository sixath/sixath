package memorysearch

import (
	"context"
	"fmt"
	"sync"

	"github.com/sixath/framework/config"
)

var (
	indexCache    = make(map[string]*MemoryIndexManager)
	fallbackCache = make(map[string]MemorySearchManager)
	indexCacheMu  sync.RWMutex
)

// GetMemorySearchManager 获取或创建 MemorySearchManager。
// embedder 可选，为 nil 时仅使用 FTS 检索。
// sessionProvider 可选，当 sources 含 "sessions" 时用于 Phase 2 会话转录。
// 若 backend 为 "qmd" 且 QMD 可用，返回 FallbackMemoryManager(QMD, builtin)；否则返回 builtin。
func GetMemorySearchManager(cfg config.Config, agentID, workspaceRoot string, embedder Embedder, sessionProvider SessionTranscriptProvider) (MemorySearchManager, error) {
	mem := cfg.Memory
	if !mem.Defaults.Enabled {
		return nil, nil
	}
	if workspaceRoot == "" {
		return nil, fmt.Errorf("memorysearch: workspace_root is required")
	}
	resolved := ResolveMemorySearch(mem.Defaults, workspaceRoot)
	if resolved == nil {
		return nil, nil
	}
	if resolved.StorePath == "" {
		return nil, fmt.Errorf("memorysearch: store path is empty")
	}

	// Phase 3: 若 backend 为 qmd，尝试 QMD
	if mem.Backend == "qmd" && mem.Qmd != nil && mem.Qmd.Command != "" {
		qmdKey := "qmd:" + agentID + ":" + workspaceRoot
		indexCacheMu.RLock()
		fb, ok := fallbackCache[qmdKey]
		indexCacheMu.RUnlock()
		if ok && fb != nil {
			return fb, nil
		}
		if qmdMgr, err := NewQmdMemoryManager(mem.Qmd, workspaceRoot); err == nil {
			if fallback, err := newFallbackMemoryManager(qmdMgr, cfg, agentID, workspaceRoot, embedder, sessionProvider); err == nil {
				indexCacheMu.Lock()
				fallbackCache[qmdKey] = fallback
				indexCacheMu.Unlock()
				return fallback, nil
			}
			indexCacheMu.Lock()
			fallbackCache[qmdKey] = qmdMgr
			indexCacheMu.Unlock()
			return qmdMgr, nil
		}
		// QMD 不可用，回退 builtin
	}

	cacheKey := agentID + ":" + workspaceRoot + ":" + resolved.StorePath
	indexCacheMu.RLock()
	mgr, ok := indexCache[cacheKey]
	indexCacheMu.RUnlock()
	if ok && mgr != nil {
		return mgr, nil
	}
	mgr, err := NewMemoryIndexManager(resolved, workspaceRoot, agentID, embedder, sessionProvider)
	if err != nil {
		return nil, err
	}
	indexCacheMu.Lock()
	indexCache[cacheKey] = mgr
	indexCacheMu.Unlock()
	return mgr, nil
}

// SyncWorkspace 对指定 workspace 执行一次同步（供上层调用）。
func SyncWorkspace(ctx context.Context, cfg config.Config, agentID, workspaceRoot string, embedder Embedder, sessionProvider SessionTranscriptProvider, force bool) error {
	mgr, err := GetMemorySearchManager(cfg, agentID, workspaceRoot, embedder, sessionProvider)
	if err != nil || mgr == nil {
		return err
	}
	return mgr.Sync(ctx, &SyncParams{Reason: "manual", Force: force})
}
