package chat

import (
	"context"
	"fmt"
	"os"
	"strings"

	"backend/internal/biz"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
)

var (
	storedConflictYAML      *config.MemoryConflict
	globalMemoryAgentGetter AgentGetter
)

// SetMemoryConflictConfig stores agent_extra memory_conflict settings.
func SetMemoryConflictConfig(cfg *config.MemoryConflict) {
	if cfg == nil {
		storedConflictYAML = nil
		return
	}
	cp := *cfg
	storedConflictYAML = &cp
}

// SetMemoryAgentGetter supplies Agent lookup for semantic-conflict model resolution.
func SetMemoryAgentGetter(g AgentGetter) {
	globalMemoryAgentGetter = g
}

func memoryConflictEnabled() bool {
	if v := strings.TrimSpace(os.Getenv("SATH_MEMORY_CONFLICT_ENABLED")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return storedConflictYAML != nil && storedConflictYAML.Enabled
}

func memoryConflictK() int {
	if storedConflictYAML != nil && storedConflictYAML.K > 0 {
		return storedConflictYAML.K
	}
	return 0
}

// memorySemanticModelFactoryAvailable is true when ResolveAdd can obtain an LLM:
// memory_extraction.auxiliary is set, or an AgentGetter can supply the chat model.
func memorySemanticModelFactoryAvailable() bool {
	if storedExtractionYAML != nil && storedExtractionYAML.Auxiliary != nil {
		if strings.TrimSpace(storedExtractionYAML.Auxiliary.Model) != "" {
			return true
		}
	}
	return globalMemoryAgentGetter != nil
}

// DefaultMemoryStoreOptions wires the dynamic semantic conflict resolver, tool switch,
// and optional units vector sidecar (P2-E).
func DefaultMemoryStoreOptions() MemoryStoreOptions {
	var resolver memory.SemanticConflictResolver
	if memorySemanticModelFactoryAvailable() {
		resolver = &dynamicSemanticConflictResolver{agents: globalMemoryAgentGetter}
	}
	var embedder memory.UnitEmbedder
	if memorySemanticModelFactoryAvailable() {
		embedder = &dynamicUnitEmbedder{agents: globalMemoryAgentGetter}
	}
	opts := MemoryStoreOptions{
		SemanticConflicts:    resolver,
		ToolSemanticConflict: memoryConflictEnabled(),
		SemanticConflictK:    memoryConflictK(),
		UnitEmbedder:         embedder,
		UnitVectors:          sharedUnitVectorIndex(),
		HybridRecall:         hybridRecallGate(globalMemoryAgentGetter),
	}
	applyMemoryVectorOptions(&opts)
	applyMemoryGraphOptions(&opts)
	return opts
}

// dynamicSemanticConflictResolver resolves an LLM at ResolveAdd time and delegates
// to memory.LLMSemanticConflictResolver (fail-closed on model resolve errors).
type dynamicSemanticConflictResolver struct {
	agents AgentGetter
}

var _ memory.SemanticConflictResolver = (*dynamicSemanticConflictResolver)(nil)

func (r *dynamicSemanticConflictResolver) ResolveAdd(ctx context.Context, candidate memory.RememberInput, peers []memory.MemoryHit) (memory.SemanticConflictVerdict, error) {
	var meta *biz.AgentMeta
	getter := globalMemoryAgentGetter
	if r != nil && r.agents != nil {
		getter = r.agents
	}
	if getter != nil && strings.TrimSpace(candidate.AgentID) != "" {
		got, err := getter.Get(ctx, candidate.AgentID)
		if err != nil {
			return memory.SemanticConflictVerdict{}, err
		}
		meta = got
	}
	m, err := resolveMemoryAuxModel(meta)
	if err != nil {
		return memory.SemanticConflictVerdict{}, err
	}
	if m == nil {
		return memory.SemanticConflictVerdict{}, fmt.Errorf("memory: semantic conflict model unavailable")
	}
	return (&memory.LLMSemanticConflictResolver{Model: m}).ResolveAdd(ctx, candidate, peers)
}

// resolveMemoryAuxModel returns the auxiliary model from MemoryExtraction config,
// or the agent's chat model. Shared by turn extract and semantic conflict.
func resolveMemoryAuxModel(agentMeta *biz.AgentMeta) (model.Model, error) {
	if storedExtractionYAML != nil && storedExtractionYAML.Auxiliary != nil {
		aux := storedExtractionYAML.Auxiliary
		if strings.TrimSpace(aux.Model) != "" {
			return BuildModel(aux.Provider, aux.Model, aux.APIKey, aux.BaseURL)
		}
	}
	if agentMeta == nil {
		return nil, nil
	}
	mc := agentMeta.ModelConfig
	return BuildModel(mc.Provider, mc.Model, mc.APIKey, mc.BaseURL)
}
