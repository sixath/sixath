package memory

import (
	"context"
	"strings"

	"github.com/sixath/framework/memory/hub/local"
)

const (
	defaultPrefetchMaxSnippets = 5
	defaultPrefetchMaxTotal    = 8
)

// StorePrefetchBackend 实现 Backend：经 MemoryStore 对 user/units、session/units 与 agent/files 三路 Recall，供 Orchestrator 注入围栏块。
type StorePrefetchBackend struct {
	Store       MemoryStore
	MaxSnippets int
	// MaxTotal caps parts after dedupe. nil → default 8; non-nil && *v<=0 → no truncate; else *v.
	MaxTotal *int
	// ProceduralBindings optional hand-written repair slots (P3-C); matched against UserMessage + AgentID.
	ProceduralBindings []ProceduralBinding
	// MaxProcedural caps procedural hint parts before merge (default 3; <=0 → 3).
	MaxProcedural int
	// LoadPersistedProcedural recalls kind=procedural units for the session (P3-E).
	LoadPersistedProcedural bool
	// OnProceduralMatched optional hook after matching procedural bindings (P3-D hit observe).
	OnProceduralMatched func(matched []ProceduralBinding)
}

// Name 实现 Backend。
func (b *StorePrefetchBackend) Name() string { return "memory_store_prefetch" }

// Prefetch 实现 Backend。
func (b *StorePrefetchBackend) Prefetch(ctx context.Context, q PrefetchQuery) ([]PrefetchPart, error) {
	if b == nil || b.Store == nil {
		return nil, nil
	}
	qText := strings.TrimSpace(q.UserMessage)
	if qText == "" {
		return nil, nil
	}
	limit := defaultPrefetchMaxSnippets
	if b.MaxSnippets > 0 {
		limit = b.MaxSnippets
	}
	parts := make([]PrefetchPart, 0, limit*2)
	var firstErr error

	if uid := strings.TrimSpace(q.UserID); uid != "" {
		userHits, err := b.Store.Recall(ctx, RecallQuery{
			Query:   qText,
			Scope:   ScopeUser,
			ScopeID: uid,
			AgentID: strings.TrimSpace(q.AgentID),
			Source:  SourceUnits,
			Limit:   limit,
		})
		if err != nil {
			firstErr = err
		} else {
			for _, h := range userHits {
				if !prefetchUnitLoadoutEligible(h) {
					continue
				}
				body := strings.TrimSpace(h.Content)
				if body == "" {
					continue
				}
				parts = append(parts, PrefetchPart{Label: "user", Content: body})
			}
		}
	}

	sessionHits, err := b.Store.Recall(ctx, RecallQuery{
		Query:     qText,
		Scope:     ScopeSession,
		ScopeID:   strings.TrimSpace(q.SessionID),
		AgentID:   strings.TrimSpace(q.AgentID),
		Source:    SourceUnits,
		Limit:     limit,
		SessionID: strings.TrimSpace(q.SessionID),
	})
	if err != nil {
		firstErr = err
	} else {
		for _, h := range sessionHits {
			if !prefetchUnitLoadoutEligible(h) {
				continue
			}
			body := strings.TrimSpace(h.Content)
			if body == "" {
				continue
			}
			parts = append(parts, PrefetchPart{Label: "session", Content: body})
		}
	}

	agentHits, err := b.Store.Recall(ctx, RecallQuery{
		Query:         qText,
		Scope:         ScopeAgent,
		AgentID:       strings.TrimSpace(q.AgentID),
		Source:        SourceFiles,
		Limit:         limit,
		WorkspaceRoot: strings.TrimSpace(q.WorkspaceRoot),
	})
	if err != nil {
		if firstErr == nil {
			firstErr = err
		}
	} else {
		for _, h := range agentHits {
			body := strings.TrimSpace(h.Content)
			if body == "" {
				continue
			}
			parts = append(parts, PrefetchPart{Label: "agent", Content: body})
		}
	}

	maxProc := b.MaxProcedural
	if maxProc <= 0 {
		maxProc = 3
	}
	binds := append([]ProceduralBinding(nil), b.ProceduralBindings...)
	if b.LoadPersistedProcedural {
		if sid := strings.TrimSpace(q.SessionID); sid != "" {
			procHits, err := b.Store.Recall(ctx, RecallQuery{
				Scope:   ScopeSession,
				ScopeID: sid,
				AgentID: strings.TrimSpace(q.AgentID),
				Source:  SourceUnits,
				Kind:    KindProcedural,
				Limit:   maxProc * 4,
			})
			if err == nil {
				var persisted []ProceduralBinding
				for _, h := range procHits {
					if bb, ok := BindingFromMetadata(h.Metadata, h.Content); ok {
						persisted = append(persisted, bb)
					}
				}
				binds = MergeProceduralBindings(binds, persisted)
			}
		}
	}
	matched := MatchProceduralBindings(binds, q.AgentID, qText, nil)
	if b.OnProceduralMatched != nil && len(matched) > 0 {
		b.OnProceduralMatched(matched)
	}
	for i, bind := range matched {
		if i >= maxProc {
			break
		}
		parts = append(parts, PrefetchPart{Label: "procedural", Content: FormatBindingSuggest(bind)})
	}

	parts = applyPrefetchQuota(parts, b.MaxTotal)
	// Prefer partial success so Orchestrator can still inject one side.
	if len(parts) > 0 {
		return parts, nil
	}
	return nil, firstErr
}

// prefetchUnitLoadoutEligible skips hub_status=draft (and other non-loadout) units.
func prefetchUnitLoadoutEligible(h MemoryHit) bool {
	dbStatus := local.UnitDBActive
	if h.Metadata != nil {
		if s, ok := h.Metadata["status"].(string); ok && strings.TrimSpace(s) != "" {
			dbStatus = strings.ToLower(strings.TrimSpace(s))
		}
	}
	return local.LoadoutEligible(local.MapUnitToAssetStatus(dbStatus, h.Metadata))
}

// applyPrefetchQuota dedupes by ContentHash(TrimSpace) (first wins) then applies max_total.
func applyPrefetchQuota(parts []PrefetchPart, maxTotal *int) []PrefetchPart {
	if len(parts) == 0 {
		return parts
	}
	seen := make(map[string]struct{}, len(parts))
	out := make([]PrefetchPart, 0, len(parts))
	for _, p := range parts {
		body := strings.TrimSpace(p.Content)
		if body == "" {
			continue
		}
		h := ContentHash(body)
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		p.Content = body
		out = append(out, p)
	}
	capN := defaultPrefetchMaxTotal
	applyCap := true
	if maxTotal != nil {
		if *maxTotal <= 0 {
			applyCap = false
		} else {
			capN = *maxTotal
		}
	}
	if applyCap && len(out) > capN {
		out = out[:capN]
	}
	return out
}
