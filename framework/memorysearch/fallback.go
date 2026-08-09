package memorysearch

import (
	"context"

	"github.com/sixath/framework/config"
)

// FallbackMemoryManager 包装主管理器，失败时回退到 builtin。
type FallbackMemoryManager struct {
	primary  MemorySearchManager
	fallback MemorySearchManager
}

// newFallbackMemoryManager 创建回退管理器。primary 为 QMD，fallback 为 builtin。
func newFallbackMemoryManager(primary MemorySearchManager, cfg config.Config, agentID, workspaceRoot string, embedder Embedder, sessionProvider SessionTranscriptProvider) (*FallbackMemoryManager, error) {
	resolved := ResolveMemorySearch(cfg.Memory.Defaults, workspaceRoot)
	if resolved == nil {
		return nil, nil
	}
	builtin, err := NewMemoryIndexManager(resolved, workspaceRoot, agentID, embedder, sessionProvider)
	if err != nil {
		return nil, err
	}
	return &FallbackMemoryManager{
		primary:  primary,
		fallback: builtin,
	}, nil
}

// Search 先尝试 primary，失败则用 fallback。
func (f *FallbackMemoryManager) Search(ctx context.Context, query string, opts *SearchOpts) ([]MemorySearchResult, error) {
	results, err := f.primary.Search(ctx, query, opts)
	if err == nil {
		return results, nil
	}
	return f.fallback.Search(ctx, query, opts)
}

// ReadFile 先尝试 primary，失败则用 fallback。
func (f *FallbackMemoryManager) ReadFile(ctx context.Context, params *ReadFileParams) (*ReadFileResult, error) {
	res, err := f.primary.ReadFile(ctx, params)
	if err == nil {
		return res, nil
	}
	return f.fallback.ReadFile(ctx, params)
}

// Status 返回 primary 状态。
func (f *FallbackMemoryManager) Status(ctx context.Context) (*MemoryProviderStatus, error) {
	st, err := f.primary.Status(ctx)
	if err == nil {
		return st, nil
	}
	return f.fallback.Status(ctx)
}

// Sync 先尝试 primary，失败则用 fallback。
func (f *FallbackMemoryManager) Sync(ctx context.Context, params *SyncParams) error {
	if err := f.primary.Sync(ctx, params); err == nil {
		return nil
	}
	return f.fallback.Sync(ctx, params)
}

// NotifySessionDirty 实现 SessionDirtyNotifier，转发到 fallback（builtin）。
func (f *FallbackMemoryManager) NotifySessionDirty(sessionID string, bytesDelta, messagesDelta int) {
	if n, ok := f.fallback.(SessionDirtyNotifier); ok {
		n.NotifySessionDirty(sessionID, bytesDelta, messagesDelta)
	}
}

// Close 释放 fallback 资源（若支持）。
func (f *FallbackMemoryManager) Close() error {
	if c, ok := f.fallback.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
}
