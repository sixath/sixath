package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	ProceduralStatusCandidate = "candidate"
	ProceduralStatusActive    = "active"
	ProceduralStatusDisabled  = "disabled"

	ProceduralHitPrefetch = "prefetch"
	ProceduralHitRouter   = "skill_router"
)

// ProceduralEntry is one repair slot under lifecycle control (P3-D).
type ProceduralEntry struct {
	ID           string
	Status       string
	SupportCount int
	Binding      ProceduralBinding
	PrefetchHits int
	RouterHits   int
	UpdatedAt    time.Time
}

// ProceduralCatalog tracks candidate/active/disabled procedural entries in-process.
type ProceduralCatalog struct {
	mu          sync.Mutex
	minSupport  int
	entries     map[string]*ProceduralEntry // id → entry
	log         *slog.Logger
	OnActivated func(entry ProceduralEntry, sig FailureSignal) // optional; P3-E auto_commit
}

// NewProceduralCatalog creates a catalog. minSupport <=0 defaults to 2.
func NewProceduralCatalog(minSupport int, log *slog.Logger) *ProceduralCatalog {
	if minSupport <= 0 {
		minSupport = 2
	}
	if log == nil {
		log = slog.Default()
	}
	return &ProceduralCatalog{
		minSupport: minSupport,
		entries:    make(map[string]*ProceduralEntry),
		log:        log,
	}
}

func proceduralEntryID(b ProceduralBinding) string {
	key := strings.Join([]string{
		b.AgentID,
		b.TriggerCode,
		b.TriggerQuery,
		b.ActionKind,
		b.SkillID,
		strings.Join(b.ToolNames, ","),
	}, "|")
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

// SeedBindings loads hand-written bindings.
// trigger_code bindings start as candidate (need FailureSignal strengthen);
// query-only bindings start as active (matchable by user text immediately).
func (c *ProceduralCatalog) SeedBindings(bindings []ProceduralBinding) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*ProceduralEntry)
	now := time.Now().UTC()
	for _, b := range bindings {
		id := proceduralEntryID(b)
		st := ProceduralStatusCandidate
		if b.TriggerCode == "" && b.TriggerQuery != "" {
			st = ProceduralStatusActive
		}
		c.entries[id] = &ProceduralEntry{
			ID:        id,
			Status:    st,
			Binding:   b,
			UpdatedAt: now,
		}
	}
}

// ObserveSignal increments support for matching trigger_code entries; promotes at minSupport.
func (c *ProceduralCatalog) ObserveSignal(sig FailureSignal) {
	if c == nil || strings.TrimSpace(sig.Code) == "" {
		return
	}
	c.mu.Lock()
	now := time.Now().UTC()
	var activated []ProceduralEntry
	for _, e := range c.entries {
		if e.Status == ProceduralStatusDisabled {
			continue
		}
		if e.Binding.TriggerCode == "" || e.Binding.TriggerCode != sig.Code {
			continue
		}
		if e.Binding.AgentID != "" && sig.AgentID != "" && e.Binding.AgentID != sig.AgentID {
			if sig.AgentName == "" || e.Binding.AgentID != sig.AgentName {
				continue
			}
		}
		e.SupportCount++
		e.UpdatedAt = now
		if e.Status == ProceduralStatusCandidate && e.SupportCount >= c.minSupport {
			e.Status = ProceduralStatusActive
			c.log.Info("procedural_entry_activated",
				"id", e.ID,
				"failure_code", e.Binding.TriggerCode,
				"agent_id", e.Binding.AgentID,
				"support_count", e.SupportCount,
			)
			activated = append(activated, *e)
		}
	}
	onAct := c.OnActivated
	c.mu.Unlock()
	if onAct != nil {
		for _, e := range activated {
			onAct(e, sig)
		}
	}
}

// Disable marks an entry disabled by id.
func (c *ProceduralCatalog) Disable(id string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[strings.TrimSpace(id)]
	if !ok {
		return false
	}
	e.Status = ProceduralStatusDisabled
	e.UpdatedAt = time.Now().UTC()
	c.log.Info("procedural_entry_disabled", "id", e.ID, "by", "id")
	return true
}

// DisableByCode disables all entries for agentID+failure_code (agentID empty matches all agents).
func (c *ProceduralCatalog) DisableByCode(agentID, failureCode string) int {
	if c == nil {
		return 0
	}
	failureCode = strings.TrimSpace(failureCode)
	agentID = strings.TrimSpace(agentID)
	if failureCode == "" {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	now := time.Now().UTC()
	for _, e := range c.entries {
		if e.Binding.TriggerCode != failureCode {
			continue
		}
		if agentID != "" && e.Binding.AgentID != "" && e.Binding.AgentID != agentID {
			continue
		}
		e.Status = ProceduralStatusDisabled
		e.UpdatedAt = now
		n++
	}
	if n > 0 {
		c.log.Info("procedural_entry_disabled", "by", "code", "failure_code", failureCode, "agent_id", agentID, "count", n)
	}
	return n
}

// ActiveBindings returns bindings that are active (for Prefetch / Skill router).
func (c *ProceduralCatalog) ActiveBindings() []ProceduralBinding {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ProceduralBinding, 0, len(c.entries))
	for _, e := range c.entries {
		if e.Status == ProceduralStatusActive {
			out = append(out, e.Binding)
		}
	}
	return out
}

// RecordHit increments hit counters for matched active bindings and logs.
func (c *ProceduralCatalog) RecordHit(channel string, matched []ProceduralBinding) {
	if c == nil || len(matched) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	for _, b := range matched {
		id := proceduralEntryID(b)
		e, ok := c.entries[id]
		if !ok || e.Status != ProceduralStatusActive {
			continue
		}
		switch channel {
		case ProceduralHitPrefetch:
			e.PrefetchHits++
		case ProceduralHitRouter:
			e.RouterHits++
		}
		e.UpdatedAt = now
		c.log.Info("procedural_entry_hit",
			"id", e.ID,
			"channel", channel,
			"prefetch_hits", e.PrefetchHits,
			"router_hits", e.RouterHits,
			"trigger_code", e.Binding.TriggerCode,
			"skill_id", e.Binding.SkillID,
		)
	}
}

// Snapshot returns a copy of all entries for tests / debug.
func (c *ProceduralCatalog) Snapshot() []ProceduralEntry {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ProceduralEntry, 0, len(c.entries))
	for _, e := range c.entries {
		cp := *e
		out = append(out, cp)
	}
	return out
}

// ProceduralCatalogSink feeds FailureSignals into a catalog.
type ProceduralCatalogSink struct {
	Catalog *ProceduralCatalog
}

func (s ProceduralCatalogSink) OnFailureSignal(_ context.Context, sig FailureSignal) {
	if s.Catalog == nil {
		return
	}
	s.Catalog.ObserveSignal(sig)
}

// EntryIDForBinding exposes stable ids for disable-by-id tests.
func EntryIDForBinding(b ProceduralBinding) string {
	return proceduralEntryID(b)
}

// String helps debugging.
func (e ProceduralEntry) String() string {
	return fmt.Sprintf("%s status=%s support=%d hits=%d/%d", e.ID, e.Status, e.SupportCount, e.PrefetchHits, e.RouterHits)
}
