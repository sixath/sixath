package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Entity is a scoped graph node in the Neo4j sidecar (MySQL units remain authoritative).
type Entity struct {
	ID             string
	Name           string
	Type           string
	Scope          Scope
	ScopeID        string
	SourceMemoryID string
	Confidence     float64
}

// Relation is a directed edge between two entities in the same scope partition.
type Relation struct {
	SubjectID      string
	Predicate      string
	ObjectID       string
	Scope          Scope
	ScopeID        string
	SourceMemoryID string
	Confidence     float64
}

// GraphExpandQuery expands neighborhood within one scope partition.
type GraphExpandQuery struct {
	SeedEntityIDs []string
	Hops          int
	Scope         Scope
	ScopeID       string
	Limit         int
}

// GraphHit is one Expand result before hydrating MySQL units.
type GraphHit struct {
	EntityID       string
	Name           string
	RelatedUnitIDs []string
	Score          float64
	Path           string
}

// GraphStore is the optional units graph sidecar (P2-I).
type GraphStore interface {
	UpsertEntity(ctx context.Context, e Entity) error
	UpsertRelation(ctx context.Context, r Relation) error
	InvalidateByMemoryID(ctx context.Context, memoryUnitID string) error
	Expand(ctx context.Context, q GraphExpandQuery) ([]GraphHit, error)
	// MatchSeeds returns entity IDs in scope whose names appear in query (case-insensitive).
	MatchSeeds(ctx context.Context, scope Scope, scopeID, query string, limit int) ([]string, error)
	// EntitiesBySourceMemoryIDs returns entities tagged with any of the given unit IDs.
	EntitiesBySourceMemoryIDs(ctx context.Context, scope Scope, scopeID string, unitIDs []string) ([]Entity, error)
	Close() error
}

// StableEntityID returns a deterministic entity id for MERGE keys.
func StableEntityID(scope Scope, scopeID, name string) string {
	norm := strings.ToLower(strings.TrimSpace(name))
	scopeID = strings.TrimSpace(scopeID)
	sum := sha256.Sum256([]byte(string(scope) + "|" + scopeID + "|" + norm))
	return hex.EncodeToString(sum[:16])
}

// NormalizeEntityName trims and lowercases for matching.
func NormalizeEntityName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
