package chat

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"backend/internal/biz"
	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/memorysearch"
	"github.com/sixath/framework/sessionsearch"
)

// memoryEmbedTripped is the process-wide embed circuit breaker shared by Facade
// (online write/recall) and UnitBackfiller. RebuildPrefetchMemoryOrchestrator
// reuses this pointer — rebuilding Prefetch Facade does not reset the breaker.
var memoryEmbedTripped = &atomic.Bool{}

// MemoryStoreOptions configures optional Facade semantic conflict, vector, and graph wiring.
type MemoryStoreOptions struct {
	SemanticConflicts    memory.SemanticConflictResolver
	ToolSemanticConflict bool
	SemanticConflictK    int
	Vectors              memory.VectorIndex
	Embed                memory.EmbedFunc
	VectorAsync          *bool
	Graph                memory.GraphStore
	GraphMaxHops         int
	GraphRRFK            int
	GraphAsync           *bool
	UnitVectors          memory.UnitVectorIndex
	UnitEmbedder         memory.UnitEmbedder
	HybridRecall         func(context.Context, string) bool
}

// BuildMemoryStore assembles Portal's session-unit, agent-workspace, and
// cross-session transcript backends into one scoped MemoryStore.
func BuildMemoryStore(session memory.SessionUnitsBackend, memoryCfg *config.MemoryConfig, sessionProvider memorysearch.SessionTranscriptProvider, opts ...MemoryStoreOptions) memory.MemoryStore {
	if session == nil {
		session = memory.NewSessionMemory()
	}

	cfg := config.Config{Memory: DefaultMemoryConfig}
	if memoryCfg != nil {
		cfg.Memory = *memoryCfg
	}
	agent := memory.NewAgentWorkspace(func(ctx context.Context, agentID, workspaceRoot string) (memorysearch.MemorySearchManager, error) {
		return memorysearch.GetMemorySearchManager(cfg, agentID, workspaceRoot, nil, sessionProvider)
	})

	sessionCfg := config.Config{SessionSearch: DefaultSessionSearchConfig}
	transcript := memory.NewSessionTranscript(func(_ context.Context, agentID string) (sessionsearch.SessionSearchManager, error) {
		if !sessionCfg.SessionSearch.Enabled {
			return nil, nil
		}
		return sessionsearch.GetSessionSearchManager(sessionCfg, agentID)
	})

	var o MemoryStoreOptions
	if len(opts) > 0 {
		o = opts[0]
	}

	return memory.NewFacade(memory.FacadeConfig{
		Session:              session,
		Agent:                agent,
		Transcript:           transcript,
		SemanticConflicts:    o.SemanticConflicts,
		ToolSemanticConflict: o.ToolSemanticConflict,
		SemanticConflictK:    o.SemanticConflictK,
		Vectors:              o.Vectors,
		Embed:                o.Embed,
		VectorAsync:          o.VectorAsync,
		Graph:                o.Graph,
		GraphMaxHops:         o.GraphMaxHops,
		GraphRRFK:            o.GraphRRFK,
		GraphAsync:           o.GraphAsync,
		UnitVectors:          o.UnitVectors,
		UnitEmbedder:         o.UnitEmbedder,
		HybridRecall:         o.HybridRecall,
		EmbedTripped:         memoryEmbedTripped,
	})
}

// GetMemoryStateSummary reads diagnostic state from the agent-workspace
// adapter for growth review. It remains in the memory wiring layer so callers
// outside this package do not depend on memorysearch.
func GetMemoryStateSummary(ctx context.Context, sessionID string, chatUC *biz.ChatUsecase, agentGetter AgentGetter) (string, error) {
	if chatUC == nil || agentGetter == nil || sessionID == "" {
		return "", nil
	}
	session, err := chatUC.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	agentMeta, err := agentGetter.Get(ctx, session.AgentID)
	if err != nil {
		return "", err
	}
	cfg := config.Config{Memory: DefaultMemoryConfig}
	provider := NewChatTranscriptProvider(chatUC)
	mgr, err := memorysearch.GetMemorySearchManager(cfg, session.AgentID, agentMeta.Workspace, nil, provider)
	if err != nil || mgr == nil {
		return "", err
	}
	st, err := mgr.Status(ctx)
	if err != nil || st == nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("backend=%s provider=%s model=%s files=%d chunks=%d cache=%d",
		st.Backend, st.Provider, st.Model, st.Files, st.Chunks, st.Cache))
	if st.Vector {
		b.WriteString(" vector=on")
	}
	if st.FTS {
		b.WriteString(" fts=on")
	}
	return b.String(), nil
}
