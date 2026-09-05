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
	proceduralAutoCommit bool
	proceduralPilots     []string
	proceduralCatalog    *memory.ProceduralCatalog
	defaultFailureSink   memory.FailureSignalSink
)

// SetProceduralRepairConfig loads hand-written bindings into the P3-D catalog (P3-C/D/E).
func SetProceduralRepairConfig(cfg *config.MemoryProceduralRepair) {
	proceduralMu.Lock()
	defer proceduralMu.Unlock()
	proceduralEnabled = false
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
}

// DefaultFailureSignalSink returns Logging+Ring(+Catalog) sink for turnBus bridges.
// Must not use sync.Once that rebuild resets — that unlocks a replaced mutex and fatals.
func DefaultFailureSignalSink() memory.FailureSignalSink {
	proceduralMu.Lock()
	defer proceduralMu.Unlock()
	if defaultFailureSink == nil {
		rebuildDefaultFailureSinkLocked()
	}
	return defaultFailureSink
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
