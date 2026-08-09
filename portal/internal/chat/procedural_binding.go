package chat

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
)

var (
	proceduralMu         sync.RWMutex
	proceduralEnabled    bool
	proceduralMax        int
	injectPrefetch       = true
	injectSkillRouter    = true
	proceduralAutoCommit bool
	proceduralPilots     []string
	proceduralCatalog    *memory.ProceduralCatalog
	defaultFailureOnce   sync.Once
	defaultFailureSink   memory.FailureSignalSink
)

// SetProceduralRepairConfig loads hand-written bindings into the P3-D catalog (P3-C/D/E).
func SetProceduralRepairConfig(cfg *config.MemoryProceduralRepair) {
	proceduralMu.Lock()
	defer proceduralMu.Unlock()
	proceduralEnabled = false
	proceduralMax = 3
	injectPrefetch = true
	injectSkillRouter = true
	proceduralAutoCommit = false
	proceduralPilots = nil
	minSupport := 2
	if cfg != nil && cfg.MinSupport > 0 {
		minSupport = cfg.MinSupport
	}
	proceduralCatalog = memory.NewProceduralCatalog(minSupport, nil)
	if cfg == nil || !cfg.Enabled {
		rebuildDefaultFailureSinkLocked()
		return
	}
	proceduralEnabled = true
	proceduralAutoCommit = cfg.AutoCommit
	proceduralPilots = append([]string(nil), cfg.PilotAgents...)
	if cfg.MaxProcedural > 0 {
		proceduralMax = cfg.MaxProcedural
	}
	if cfg.Inject != nil {
		if cfg.Inject.Prefetch != nil {
			injectPrefetch = *cfg.Inject.Prefetch
		}
		if cfg.Inject.SkillRouter != nil {
			injectSkillRouter = *cfg.Inject.SkillRouter
		}
	}
	raw := make([]memory.ProceduralBinding, 0, len(cfg.Bindings))
	defaultMode := cfg.Mode
	for _, y := range cfg.Bindings {
		mode := y.Mode
		if mode == "" {
			mode = defaultMode
		}
		raw = append(raw, memory.ProceduralBinding{
			TriggerCode:  y.TriggerCode,
			TriggerQuery: y.TriggerQuery,
			ActionKind:   y.ActionKind,
			SkillID:      y.SkillID,
			ToolNames:    y.ToolNames,
			Mode:         mode,
			AgentID:      y.AgentID,
		})
	}
	valid := memory.FilterValidBindings(raw, nil, nil)
	proceduralCatalog.SeedBindings(valid)
	if proceduralAutoCommit {
		proceduralCatalog.OnActivated = autoCommitOnActivate
	}
	rebuildDefaultFailureSinkLocked()
}

func autoCommitOnActivate(entry memory.ProceduralEntry, sig memory.FailureSignal) {
	proceduralMu.RLock()
	enabled := proceduralEnabled
	auto := proceduralAutoCommit
	pilots := append([]string(nil), proceduralPilots...)
	proceduralMu.RUnlock()
	if !enabled || !auto {
		return
	}
	store := globalPrefetchMemoryStore
	facade, ok := store.(*memory.Facade)
	if !ok || facade == nil {
		slog.Warn("procedural_auto_commit_skip", "reason", "no_facade")
		return
	}
	agentName := strings.TrimSpace(sig.AgentName)
	if agentName == "" {
		agentName = strings.TrimSpace(entry.Binding.AgentID)
	}
	_, err := facade.CommitProceduralRepair(context.Background(), memory.ProceduralCommitInput{
		AgentID:      sig.AgentID,
		AgentName:    agentName,
		SessionID:    sig.SessionID,
		PilotAgents:  pilots,
		Signal:       sig,
		Binding:      entry.Binding,
		SupportCount: entry.SupportCount,
		EntryID:      entry.ID,
	})
	if err != nil {
		slog.Warn("procedural_auto_commit_failed", "err", err.Error(), "entry_id", entry.ID, "code", sig.Code)
		return
	}
	slog.Info("procedural_auto_commit", "entry_id", entry.ID, "failure_code", sig.Code, "agent_id", sig.AgentID)
}

func rebuildDefaultFailureSinkLocked() {
	sinks := memory.MultiFailureSink{
		memory.LoggingFailureSink{},
		&memory.RingFailureSink{N: 64},
	}
	if proceduralCatalog != nil {
		sinks = append(sinks, memory.ProceduralCatalogSink{Catalog: proceduralCatalog})
	}
	defaultFailureSink = sinks
	// Allow DefaultFailureSignalSink to pick up new sink even after Once — reset Once on config change.
	defaultFailureOnce = sync.Once{}
}

// DefaultFailureSignalSink returns Logging+Ring(+Catalog) sink for turnBus bridges.
func DefaultFailureSignalSink() memory.FailureSignalSink {
	proceduralMu.Lock()
	defer proceduralMu.Unlock()
	defaultFailureOnce.Do(func() {
		if defaultFailureSink == nil {
			rebuildDefaultFailureSinkLocked()
		}
	})
	return defaultFailureSink
}

func catalogActiveBindingsLocked() []memory.ProceduralBinding {
	if proceduralCatalog == nil {
		return nil
	}
	return proceduralCatalog.ActiveBindings()
}

func loadPersistedProceduralBindings(agentID, sessionID string) []memory.ProceduralBinding {
	store := globalPrefetchMemoryStore
	if store == nil || sessionID == "" {
		return nil
	}
	hits, err := store.Recall(context.Background(), memory.RecallQuery{
		Scope:   memory.ScopeSession,
		ScopeID: sessionID,
		AgentID: agentID,
		Source:  memory.SourceUnits,
		Kind:    memory.KindProcedural,
		Limit:   32,
	})
	if err != nil || len(hits) == 0 {
		return nil
	}
	out := make([]memory.ProceduralBinding, 0, len(hits))
	for _, h := range hits {
		b, ok := memory.BindingFromMetadata(h.Metadata, h.Content)
		if !ok {
			continue
		}
		out = append(out, b)
	}
	return out
}

// ProceduralBindingsForPrefetch returns active catalog + persisted bindings when inject.prefetch is on.
func ProceduralBindingsForPrefetch() ([]memory.ProceduralBinding, int, *memory.ProceduralCatalog) {
	proceduralMu.RLock()
	defer proceduralMu.RUnlock()
	if !proceduralEnabled || !injectPrefetch || proceduralCatalog == nil {
		return nil, 0, nil
	}
	return catalogActiveBindingsLocked(), proceduralMax, proceduralCatalog
}

func proceduralPrefetchEnabled() bool {
	proceduralMu.RLock()
	defer proceduralMu.RUnlock()
	return proceduralEnabled && injectPrefetch
}

// ProceduralBindingsForTurn merges catalog active bindings with persisted procedural units for this session.
func ProceduralBindingsForTurn(agentID, sessionID string) ([]memory.ProceduralBinding, int, *memory.ProceduralCatalog) {
	proceduralMu.RLock()
	enabled := proceduralEnabled
	prefetchOn := injectPrefetch
	maxP := proceduralMax
	cat := proceduralCatalog
	var catalogBinds []memory.ProceduralBinding
	if enabled && prefetchOn && cat != nil {
		catalogBinds = cat.ActiveBindings()
	}
	proceduralMu.RUnlock()
	if !enabled || !prefetchOn {
		return nil, 0, nil
	}
	persisted := loadPersistedProceduralBindings(agentID, sessionID)
	return memory.MergeProceduralBindings(catalogBinds, persisted), maxP, cat
}

// ProceduralBindingsForSkillRouter returns active bindings when inject.skill_router is on.
func ProceduralBindingsForSkillRouter() ([]memory.ProceduralBinding, *memory.ProceduralCatalog) {
	proceduralMu.RLock()
	defer proceduralMu.RUnlock()
	if !proceduralEnabled || !injectSkillRouter || proceduralCatalog == nil {
		return nil, nil
	}
	return proceduralCatalog.ActiveBindings(), proceduralCatalog
}

// ProceduralBindingsForSkillRouterTurn merges catalog + persisted for skill router.
func ProceduralBindingsForSkillRouterTurn(agentID, sessionID string) ([]memory.ProceduralBinding, *memory.ProceduralCatalog) {
	proceduralMu.RLock()
	enabled := proceduralEnabled
	routerOn := injectSkillRouter
	cat := proceduralCatalog
	var catalogBinds []memory.ProceduralBinding
	if enabled && routerOn && cat != nil {
		catalogBinds = cat.ActiveBindings()
	}
	proceduralMu.RUnlock()
	if !enabled || !routerOn {
		return nil, nil
	}
	persisted := loadPersistedProceduralBindings(agentID, sessionID)
	return memory.MergeProceduralBindings(catalogBinds, persisted), cat
}

// DisableProceduralEntry disables by entry id (P3-D).
func DisableProceduralEntry(id string) bool {
	proceduralMu.RLock()
	cat := proceduralCatalog
	proceduralMu.RUnlock()
	if cat == nil {
		return false
	}
	return cat.Disable(id)
}

// DisableProceduralByCode disables by failure_code (+ optional agent_id).
func DisableProceduralByCode(agentID, failureCode string) int {
	proceduralMu.RLock()
	cat := proceduralCatalog
	proceduralMu.RUnlock()
	if cat == nil {
		return 0
	}
	return cat.DisableByCode(agentID, failureCode)
}
