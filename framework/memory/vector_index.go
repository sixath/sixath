package memory

import (
	"context"
)

// UnitVectorRecord is one units-row embedding in the vector sidecar.
type UnitVectorRecord struct {
	UnitID    string
	Scope     Scope
	ScopeID   string
	Embedding []float32
}

// VectorSearchQuery searches the sidecar within one scope namespace.
type VectorSearchQuery struct {
	Scope     Scope
	ScopeID   string
	Embedding []float32
	Limit     int
}

// ScoredUnitID is a Search hit before hydrating MySQL content.
type ScoredUnitID struct {
	UnitID string
	Score  float64
}

// VectorIndex is the optional units vector sidecar (MySQL remains authoritative).
type VectorIndex interface {
	Upsert(ctx context.Context, rec UnitVectorRecord) error
	Delete(ctx context.Context, memoryUnitID string) error
	Search(ctx context.Context, q VectorSearchQuery) ([]ScoredUnitID, error)
	Close() error
}

// EmbedFunc embeds texts for indexing and query-time search.
type EmbedFunc func(ctx context.Context, texts []string) ([][]float32, error)
