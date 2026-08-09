package memory

import (
	"context"
	"errors"
	"sort"
	"sync"
)

// UnitVectorIndex is a pluggable vector sidecar for memory_units (session/user scopes).
// Implementations must isolate results by (Scope, ScopeID) and treat
// (Scope, ScopeID, UnitID) as the upsert primary key.
type UnitVectorIndex interface {
	Upsert(ctx context.Context, rec UnitVectorEntry) error
	Delete(ctx context.Context, scope Scope, scopeID string, unitIDs ...string) error
	Search(ctx context.Context, q UnitVectorQuery) ([]UnitVectorHit, error)
	// Has reports which unitIDs already exist under (scope, scopeID).
	// Empty unitIDs returns an empty map. Results must not leak across scopes.
	Has(ctx context.Context, scope Scope, scopeID string, unitIDs []string) (map[string]bool, error)
	Close() error
}

// ErrVectorDimMismatch is returned when an Upsert/Search vector length disagrees
// with the index dimension baseline.
var ErrVectorDimMismatch = errors.New("memory: vector dimension mismatch")

// ErrEmbedModelUnavailable is returned when no embed model can be resolved for a
// unit (Backfiller treats this as Skipped; online embedOne still trips).
var ErrEmbedModelUnavailable = errors.New("memory: embed model unavailable")

// UnitVectorEntry is one units-row embedding in the SQLite hybrid sidecar.
// Named distinctly from vector_index.go's UnitVectorRecord (Qdrant sidecar, P2-H).
type UnitVectorEntry struct {
	Scope   Scope
	ScopeID string
	UnitID  string
	Vector  []float32
}

type UnitVectorQuery struct {
	Scope    Scope
	ScopeID  string
	Vector   []float32
	Limit    int
	MinScore float64
}

// UnitVectorHit carries cosine similarity in [-1, 1]; providers must not use other scales.
type UnitVectorHit struct {
	UnitID string
	Score  float64
}

// UnitEmbedder embeds unit text. agentID lets Portal resolve the model per call
// (memory_extraction.auxiliary, else the agent chat model).
type UnitEmbedder interface {
	Embed(ctx context.Context, agentID string, texts []string) ([][]float32, error)
}

type unitVectorKey struct {
	scope   Scope
	scopeID string
	unitID  string
}

// InMemoryUnitVectorIndex is the reference provider used by tests.
type InMemoryUnitVectorIndex struct {
	mu      sync.RWMutex
	vectors map[unitVectorKey][]float32
}

func NewInMemoryUnitVectorIndex() *InMemoryUnitVectorIndex {
	return &InMemoryUnitVectorIndex{vectors: make(map[unitVectorKey][]float32)}
}

var _ UnitVectorIndex = (*InMemoryUnitVectorIndex)(nil)

func (s *InMemoryUnitVectorIndex) Upsert(_ context.Context, rec UnitVectorEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	vec := make([]float32, len(rec.Vector))
	copy(vec, rec.Vector)
	s.vectors[unitVectorKey{rec.Scope, rec.ScopeID, rec.UnitID}] = vec
	return nil
}

func (s *InMemoryUnitVectorIndex) Delete(_ context.Context, scope Scope, scopeID string, unitIDs ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range unitIDs {
		delete(s.vectors, unitVectorKey{scope, scopeID, id})
	}
	return nil
}

func (s *InMemoryUnitVectorIndex) Search(_ context.Context, q UnitVectorQuery) ([]UnitVectorHit, error) {
	if q.Limit <= 0 || len(q.Vector) == 0 {
		return nil, nil
	}
	s.mu.RLock()
	hits := make([]UnitVectorHit, 0, len(s.vectors))
	for k, v := range s.vectors {
		if k.scope != q.Scope || k.scopeID != q.ScopeID {
			continue
		}
		score := float64(cosineSimilarity(v, q.Vector))
		if q.MinScore != 0 && score < q.MinScore {
			continue
		}
		hits = append(hits, UnitVectorHit{UnitID: k.unitID, Score: score})
	}
	s.mu.RUnlock()

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].UnitID < hits[j].UnitID
		}
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > q.Limit {
		hits = hits[:q.Limit]
	}
	return hits, nil
}

func (s *InMemoryUnitVectorIndex) Has(_ context.Context, scope Scope, scopeID string, unitIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(unitIDs))
	if len(unitIDs) == 0 {
		return out, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, id := range unitIDs {
		if _, ok := s.vectors[unitVectorKey{scope, scopeID, id}]; ok {
			out[id] = true
		}
	}
	return out, nil
}

func (s *InMemoryUnitVectorIndex) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vectors = make(map[unitVectorKey][]float32)
	return nil
}
