//go:build neo4j_live

package memory

import (
	"context"
	"os"
	"testing"
	"time"
)

// Live Neo4j smoke: Upsert → Expand same scope → cross-scope isolation → Invalidate.
func TestNeo4jLiveUpsertExpandInvalidate(t *testing.T) {
	pass := os.Getenv("NEO4J_PASSWORD")
	if pass == "" {
		t.Skip("set NEO4J_PASSWORD for live neo4j e2e")
	}
	store, err := NewNeo4jGraphStore(Neo4jConfig{
		URI:      "bolt://127.0.0.1:7687",
		Username: "neo4j",
		Password: pass,
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	scopeID := "e2e-graph-" + time.Now().Format("150405")
	alice := Entity{
		ID: StableEntityID(ScopeSession, scopeID, "Alice"),
		Name: "Alice", Type: "person", Scope: ScopeSession, ScopeID: scopeID,
		SourceMemoryID: "unit-alice", Confidence: 0.95,
	}
	acme := Entity{
		ID: StableEntityID(ScopeSession, scopeID, "Acme"),
		Name: "Acme", Type: "org", Scope: ScopeSession, ScopeID: scopeID,
		SourceMemoryID: "unit-acme", Confidence: 0.95,
	}
	other := Entity{
		ID: StableEntityID(ScopeSession, "other-scope", "Alice"),
		Name: "Alice", Type: "person", Scope: ScopeSession, ScopeID: "other-scope",
		SourceMemoryID: "unit-other", Confidence: 0.95,
	}
	if err := store.UpsertEntity(ctx, alice); err != nil {
		t.Fatalf("upsert alice: %v", err)
	}
	if err := store.UpsertEntity(ctx, acme); err != nil {
		t.Fatalf("upsert acme: %v", err)
	}
	if err := store.UpsertEntity(ctx, other); err != nil {
		t.Fatalf("upsert other: %v", err)
	}
	if err := store.UpsertRelation(ctx, Relation{
		SubjectID: alice.ID, ObjectID: acme.ID, Predicate: "works_at",
		Scope: ScopeSession, ScopeID: scopeID, SourceMemoryID: "unit-rel", Confidence: 0.9,
	}); err != nil {
		t.Fatalf("upsert rel: %v", err)
	}

	hits, err := store.Expand(ctx, GraphExpandQuery{
		Scope: ScopeSession, ScopeID: scopeID, SeedEntityIDs: []string{alice.ID}, Hops: 1, Limit: 10,
	})
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	foundAcme := false
	for _, h := range hits {
		if h.EntityID == acme.ID || h.Name == "Acme" {
			foundAcme = true
		}
		if h.EntityID == other.ID {
			t.Fatalf("cross-scope leak: %+v", h)
		}
	}
	if !foundAcme {
		t.Fatalf("expand missed Acme: %+v", hits)
	}

	if err := store.InvalidateByMemoryID(ctx, "unit-rel"); err != nil {
		t.Fatalf("invalidate rel: %v", err)
	}
	if err := store.InvalidateByMemoryID(ctx, "unit-alice"); err != nil {
		t.Fatalf("invalidate alice: %v", err)
	}
	if err := store.InvalidateByMemoryID(ctx, "unit-acme"); err != nil {
		t.Fatalf("invalidate acme: %v", err)
	}
	hits2, err := store.Expand(ctx, GraphExpandQuery{
		Scope: ScopeSession, ScopeID: scopeID, SeedEntityIDs: []string{alice.ID}, Hops: 1, Limit: 10,
	})
	if err != nil {
		t.Fatalf("expand after invalidate: %v", err)
	}
	if len(hits2) != 0 {
		t.Fatalf("expected empty expand after invalidate, got %+v", hits2)
	}
}
