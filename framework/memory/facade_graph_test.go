package memory

import (
	"context"
	"strings"
	"testing"
)

func TestFacade_GraphInvalidateOnDelete(t *testing.T) {
	g := newFakeGraphStore()
	async := false
	f := NewFacade(FacadeConfig{
		Session:    NewSessionMemory(),
		Graph:      g,
		GraphAsync: &async,
	})
	ctx := context.Background()
	hit, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "Alice works at Acme",
	})
	if err != nil {
		t.Fatal(err)
	}
	eid := StableEntityID(ScopeSession, "s1", "Alice")
	_ = g.UpsertEntity(ctx, Entity{
		ID: eid, Name: "Alice", Scope: ScopeSession, ScopeID: "s1", SourceMemoryID: hit.ID,
	})
	if err := f.Delete(ctx, GetRef{Scope: ScopeSession, ScopeID: "s1", ID: hit.ID}); err != nil {
		t.Fatal(err)
	}
	ents, _ := g.EntitiesBySourceMemoryIDs(ctx, ScopeSession, "s1", []string{hit.ID})
	if len(ents) != 0 {
		t.Fatalf("expected invalidate, got %#v", ents)
	}
}

func TestFacade_GraphRRFRecall(t *testing.T) {
	g := newFakeGraphStore()
	async := false
	sess := NewSessionMemory()
	f := NewFacade(FacadeConfig{
		Session:      sess,
		Graph:        g,
		GraphAsync:   &async,
		GraphMaxHops: 1,
		GraphRRFK:    60,
	})
	ctx := context.Background()

	a, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "Alice is an engineer",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "Acme builds rockets",
	})
	if err != nil {
		t.Fatal(err)
	}

	aliceID := StableEntityID(ScopeSession, "s1", "Alice")
	acmeID := StableEntityID(ScopeSession, "s1", "Acme")
	_ = g.UpsertEntity(ctx, Entity{ID: aliceID, Name: "Alice", Scope: ScopeSession, ScopeID: "s1", SourceMemoryID: a.ID})
	_ = g.UpsertEntity(ctx, Entity{ID: acmeID, Name: "Acme", Scope: ScopeSession, ScopeID: "s1", SourceMemoryID: b.ID})
	_ = g.UpsertRelation(ctx, Relation{
		SubjectID: aliceID, Predicate: "works_at", ObjectID: acmeID,
		Scope: ScopeSession, ScopeID: "s1", SourceMemoryID: a.ID, Confidence: 0.9,
	})

	hits, err := f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "tell me about Alice", Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	foundAcme := false
	for _, h := range hits {
		if h.ID == b.ID || strings.Contains(h.Content, "Acme") {
			foundAcme = true
		}
	}
	if !foundAcme {
		t.Fatalf("expected graph-expanded Acme unit, got %#v", hits)
	}
}

func TestFacade_GraphErrorFallsBack(t *testing.T) {
	async := false
	f := NewFacade(FacadeConfig{
		Session:    NewSessionMemory(),
		Graph:      &errGraphStore{},
		GraphAsync: &async,
	})
	ctx := context.Background()
	_, _ = f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "plain fact about widgets",
	})
	hits, err := f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "widgets", Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected LIKE fallback hits")
	}
}

type errGraphStore struct{}

func (e *errGraphStore) UpsertEntity(context.Context, Entity) error   { return nil }
func (e *errGraphStore) UpsertRelation(context.Context, Relation) error { return nil }
func (e *errGraphStore) InvalidateByMemoryID(context.Context, string) error {
	return nil
}
func (e *errGraphStore) Expand(context.Context, GraphExpandQuery) ([]GraphHit, error) {
	return nil, context.Canceled
}
func (e *errGraphStore) MatchSeeds(context.Context, Scope, string, string, int) ([]string, error) {
	return nil, context.Canceled
}
func (e *errGraphStore) EntitiesBySourceMemoryIDs(context.Context, Scope, string, []string) ([]Entity, error) {
	return nil, context.Canceled
}
func (e *errGraphStore) Close() error { return nil }

var _ GraphStore = (*errGraphStore)(nil)
var _ GraphStore = (*fakeGraphStore)(nil)
var _ GraphStore = (*Neo4jGraphStore)(nil)
