package memory

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultBackfillBatchSize = 50
	defaultBackfillSleep     = 200 * time.Millisecond
)

// BackfillConfig configures UnitBackfiller.
type BackfillConfig struct {
	// Units MUST be SessionUnitsBackend (not Facade): Facade.List(ScopeUser, "") returns empty.
	Units        SessionUnitsBackend
	Index        UnitVectorIndex
	Embedder     UnitEmbedder
	Force        bool
	DryRun       bool
	BatchSize    int
	BatchSleep   time.Duration
	Scopes       []Scope
	EmbedTripped *atomic.Bool // nil → owned by backfiller
}

// BackfillStats summarizes one Run.
type BackfillStats struct {
	Scanned  int
	Missing  int
	Upserted int
	Skipped  int
	Failed   int
	Tripped  bool
}

// UnitBackfiller fills or rebuilds unit vectors from active memory units.
type UnitBackfiller struct {
	cfg     BackfillConfig
	tripped *atomic.Bool
}

// NewUnitBackfiller applies defaults and returns a ready runner.
func NewUnitBackfiller(cfg BackfillConfig) *UnitBackfiller {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultBackfillBatchSize
	}
	// BatchSleep: 0 means no sleep (tests). Portal/CLI set defaultBackfillSleep (200ms).
	if cfg.BatchSleep < 0 {
		cfg.BatchSleep = 0
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []Scope{ScopeSession, ScopeUser}
	}
	tripped := cfg.EmbedTripped
	if tripped == nil {
		tripped = &atomic.Bool{}
	}
	return &UnitBackfiller{cfg: cfg, tripped: tripped}
}

// Run scans active units and upserts missing (or all, if Force) vectors.
//
// Embed error contract:
//   - nil result with empty vector → trip breaker, return (stats, nil)
//   - errors.Is(err, ErrEmbedModelUnavailable) → Skipped++ (continue)
//   - any other Embed error → trip breaker, return (stats, nil)
func (b *UnitBackfiller) Run(ctx context.Context) (BackfillStats, error) {
	var stats BackfillStats
	if b == nil || b.cfg.Units == nil || b.cfg.Index == nil || b.cfg.Embedder == nil {
		return stats, errors.New("memory: backfill requires Units, Index, and Embedder")
	}
	for _, scope := range b.cfg.Scopes {
		if err := b.runScope(ctx, scope, &stats); err != nil {
			return stats, err
		}
		if stats.Tripped {
			return stats, nil
		}
	}
	return stats, nil
}

func (b *UnitBackfiller) runScope(ctx context.Context, scope Scope, stats *BackfillStats) error {
	offset := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := b.cfg.Units.List(ctx, ListFilter{
			Scope:  scope,
			Status: "active",
			Limit:  b.cfg.BatchSize,
			Offset: offset,
		})
		if err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}
		stats.Scanned += len(page)

		type group struct {
			hits []MemoryHit
		}
		groups := map[string]*group{}
		order := make([]string, 0)
		for _, hit := range page {
			sid := deriveScopeID(scope, hit)
			if sid == "" {
				stats.Skipped++
				continue
			}
			g, ok := groups[sid]
			if !ok {
				g = &group{}
				groups[sid] = g
				order = append(order, sid)
			}
			g.hits = append(g.hits, hit)
		}

		for _, sid := range order {
			g := groups[sid]
			ids := make([]string, 0, len(g.hits))
			for _, h := range g.hits {
				ids = append(ids, h.ID)
			}
			present, err := b.cfg.Index.Has(ctx, scope, sid, ids)
			if err != nil {
				return err
			}
			for _, hit := range g.hits {
				if !b.cfg.Force && present[hit.ID] {
					continue
				}
				stats.Missing++
				if b.cfg.DryRun {
					continue
				}
				if strings.TrimSpace(hit.Content) == "" {
					stats.Skipped++
					continue
				}
				if b.tripped.Load() {
					stats.Tripped = true
					return nil
				}
				agentID := metaString(hit.Metadata, "agent_id")
				vecs, err := b.cfg.Embedder.Embed(ctx, agentID, []string{hit.Content})
				if err != nil {
					if errors.Is(err, ErrEmbedModelUnavailable) {
						stats.Skipped++
						continue
					}
					b.tripped.Store(true)
					stats.Tripped = true
					return nil
				}
				if len(vecs) == 0 || len(vecs[0]) == 0 {
					b.tripped.Store(true)
					stats.Tripped = true
					return nil
				}
				err = b.cfg.Index.Upsert(ctx, UnitVectorEntry{
					Scope:   scope,
					ScopeID: sid,
					UnitID:  hit.ID,
					Vector:  vecs[0],
				})
				if err != nil {
					if errors.Is(err, ErrVectorDimMismatch) {
						return err
					}
					stats.Failed++
					continue
				}
				stats.Upserted++
			}
		}

		offset += len(page)
		if len(page) < b.cfg.BatchSize {
			return nil
		}
		if b.cfg.BatchSleep > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(b.cfg.BatchSleep):
			}
		}
	}
}

func deriveScopeID(scope Scope, hit MemoryHit) string {
	switch scope {
	case ScopeSession:
		return strings.TrimSpace(metaString(hit.Metadata, "source_session_id"))
	case ScopeUser:
		return strings.TrimSpace(metaString(hit.Metadata, "user_id"))
	default:
		return ""
	}
}

func metaString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}
