package chat

import (
	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/memorysearch"
)

var (
	storedPrefetchYAML            *config.MemoryOrchestratorPrefetch
	globalPrefetchSessionProvider memorysearch.SessionTranscriptProvider
	globalPrefetchMemoryStore     memory.MemoryStore
	prefetchMemoryOrchestrator    *memory.Orchestrator
)

// SetPrefetchSessionProvider 供记忆预取 Backend 使用（Phase 2 sessions 源）；可由 NewChatService 注入。
func SetPrefetchSessionProvider(p memorysearch.SessionTranscriptProvider) {
	globalPrefetchSessionProvider = p
}

// SetPrefetchMemoryStore supplies the same facade used by runtime memory tools.
func SetPrefetchMemoryStore(store memory.MemoryStore) {
	globalPrefetchMemoryStore = store
}

// BuildPrefetchMemoryOrchestrator 根据 agent_extra 形状装配 Orchestrator；未启用或配置无效时返回 nil。
func BuildPrefetchMemoryOrchestrator(ym *config.MemoryOrchestratorPrefetch) *memory.Orchestrator {
	if ym == nil || !ym.Enabled {
		return nil
	}
	o := memory.NewOrchestrator()
	o.PrefetchTimeoutMS = ym.PrefetchTimeoutMS
	o.PrefetchFailClosed = ym.PrefetchFailClosed
	if s := ym.FenceTag; s != "" {
		o.FenceTag = s
	}
	store := globalPrefetchMemoryStore
	if store == nil {
		store = BuildMemoryStore(nil, nil, globalPrefetchSessionProvider, DefaultMemoryStoreOptions())
	}
	b := &memory.StorePrefetchBackend{Store: store, MaxSnippets: ym.MaxSnippets}
	if ym.MaxTotal != nil {
		v := *ym.MaxTotal
		b.MaxTotal = &v
	}
	if err := o.RegisterBackend(b); err != nil {
		return nil
	}
	return o
}

// RebuildPrefetchMemoryOrchestrator 在 SessionProvider 等依赖就绪后重建全局预取 Orchestrator（幂等）。
func RebuildPrefetchMemoryOrchestrator() {
	if storedPrefetchYAML == nil {
		prefetchMemoryOrchestrator = nil
		return
	}
	prefetchMemoryOrchestrator = BuildPrefetchMemoryOrchestrator(storedPrefetchYAML)
}
