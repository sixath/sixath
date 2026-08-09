package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fakeGraphStore is an in-memory GraphStore for unit tests.
type fakeGraphStore struct {
	mu        sync.Mutex
	entities  map[string]Entity
	relations []Relation
}

func newFakeGraphStore() *fakeGraphStore {
	return &fakeGraphStore{entities: make(map[string]Entity)}
}

func (f *fakeGraphStore) UpsertEntity(_ context.Context, e Entity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e.ID == "" {
		e.ID = StableEntityID(e.Scope, e.ScopeID, e.Name)
	}
	f.entities[e.ID] = e
	return nil
}

func (f *fakeGraphStore) UpsertRelation(_ context.Context, r Relation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, existing := range f.relations {
		if existing.SubjectID == r.SubjectID && existing.ObjectID == r.ObjectID &&
			existing.Predicate == r.Predicate && existing.Scope == r.Scope && existing.ScopeID == r.ScopeID {
			f.relations[i] = r
			return nil
		}
	}
	f.relations = append(f.relations, r)
	return nil
}

func (f *fakeGraphStore) InvalidateByMemoryID(_ context.Context, memoryUnitID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	memoryUnitID = strings.TrimSpace(memoryUnitID)
	kept := f.relations[:0]
	for _, r := range f.relations {
		if r.SourceMemoryID != memoryUnitID {
			kept = append(kept, r)
		}
	}
	f.relations = kept
	for id, e := range f.entities {
		if e.SourceMemoryID == memoryUnitID {
			delete(f.entities, id)
		}
	}
	return nil
}

func (f *fakeGraphStore) Expand(_ context.Context, q GraphExpandQuery) ([]GraphHit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(q.SeedEntityIDs) == 0 || q.Hops <= 0 {
		return nil, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	type nodeDist struct {
		id   string
		dist int
	}
	seen := map[string]int{}
	var queue []nodeDist
	for _, id := range q.SeedEntityIDs {
		queue = append(queue, nodeDist{id: id, dist: 0})
		seen[id] = 0
	}
	for qi := 0; qi < len(queue); qi++ {
		cur := queue[qi]
		if cur.dist >= q.Hops {
			continue
		}
		for _, r := range f.relations {
			if r.Scope != q.Scope || r.ScopeID != q.ScopeID {
				continue
			}
			var next string
			if r.SubjectID == cur.id {
				next = r.ObjectID
			} else if r.ObjectID == cur.id {
				next = r.SubjectID
			} else {
				continue
			}
			nd := cur.dist + 1
			if prev, ok := seen[next]; ok && prev <= nd {
				continue
			}
			seen[next] = nd
			queue = append(queue, nodeDist{id: next, dist: nd})
		}
	}
	seedSet := map[string]struct{}{}
	for _, id := range q.SeedEntityIDs {
		seedSet[id] = struct{}{}
	}
	var hits []GraphHit
	for id, dist := range seen {
		if _, isSeed := seedSet[id]; isSeed {
			continue
		}
		e, ok := f.entities[id]
		if !ok || e.Scope != q.Scope || e.ScopeID != q.ScopeID {
			continue
		}
		hit := GraphHit{EntityID: id, Name: e.Name, Score: 1.0 / float64(dist+1)}
		if e.SourceMemoryID != "" {
			hit.RelatedUnitIDs = []string{e.SourceMemoryID}
		}
		hits = append(hits, hit)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func (f *fakeGraphStore) MatchSeeds(_ context.Context, scope Scope, scopeID, query string, limit int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := NormalizeEntityName(query)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	var out []string
	for _, e := range f.entities {
		if e.Scope != scope || e.ScopeID != scopeID {
			continue
		}
		n := NormalizeEntityName(e.Name)
		if strings.Contains(q, n) || strings.Contains(n, q) {
			out = append(out, e.ID)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeGraphStore) EntitiesBySourceMemoryIDs(_ context.Context, scope Scope, scopeID string, unitIDs []string) ([]Entity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := map[string]struct{}{}
	for _, id := range unitIDs {
		want[id] = struct{}{}
	}
	var out []Entity
	for _, e := range f.entities {
		if e.Scope != scope || e.ScopeID != scopeID {
			continue
		}
		if _, ok := want[e.SourceMemoryID]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeGraphStore) Close() error { return nil }

func TestStableEntityID_Deterministic(t *testing.T) {
	a := StableEntityID(ScopeUser, "u1", "Alice")
	b := StableEntityID(ScopeUser, "u1", " alice ")
	if a != b || a == "" {
		t.Fatalf("want equal stable ids, got %q %q", a, b)
	}
	c := StableEntityID(ScopeSession, "s1", "Alice")
	if a == c {
		t.Fatal("different scope must differ")
	}
}

func TestFakeGraphStore_UpsertExpandInvalidate(t *testing.T) {
	g := newFakeGraphStore()
	ctx := context.Background()
	alice := Entity{Name: "Alice", Type: "person", Scope: ScopeSession, ScopeID: "s1", SourceMemoryID: "u1", Confidence: 0.9}
	acme := Entity{Name: "Acme", Type: "org", Scope: ScopeSession, ScopeID: "s1", SourceMemoryID: "u2", Confidence: 0.9}
	other := Entity{Name: "Bob", Type: "person", Scope: ScopeSession, ScopeID: "s2", SourceMemoryID: "u9", Confidence: 0.9}
	_ = g.UpsertEntity(ctx, alice)
	_ = g.UpsertEntity(ctx, acme)
	_ = g.UpsertEntity(ctx, other)
	alice.ID = StableEntityID(alice.Scope, alice.ScopeID, alice.Name)
	acme.ID = StableEntityID(acme.Scope, acme.ScopeID, acme.Name)
	_ = g.UpsertRelation(ctx, Relation{
		SubjectID: alice.ID, Predicate: "works_at", ObjectID: acme.ID,
		Scope: ScopeSession, ScopeID: "s1", SourceMemoryID: "u1", Confidence: 0.95,
	})
	other.ID = StableEntityID(other.Scope, other.ScopeID, other.Name)
	_ = g.UpsertRelation(ctx, Relation{
		SubjectID: other.ID, Predicate: "knows", ObjectID: StableEntityID(ScopeSession, "s2", "Acme"),
		Scope: ScopeSession, ScopeID: "s2", SourceMemoryID: "u9", Confidence: 0.9,
	})

	hits, err := g.Expand(ctx, GraphExpandQuery{
		SeedEntityIDs: []string{alice.ID}, Hops: 1, Scope: ScopeSession, ScopeID: "s1", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].EntityID != acme.ID {
		t.Fatalf("want Acme in s1, got %#v", hits)
	}
	for _, h := range hits {
		if h.EntityID == other.ID {
			t.Fatalf("leaked other scope: %#v", hits)
		}
	}

	if err := g.InvalidateByMemoryID(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	hits, err = g.Expand(ctx, GraphExpandQuery{
		SeedEntityIDs: []string{alice.ID}, Hops: 1, Scope: ScopeSession, ScopeID: "s1", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected empty after invalidate, got %#v", hits)
	}
}

func TestFakeGraphStore_MatchSeeds(t *testing.T) {
	g := newFakeGraphStore()
	ctx := context.Background()
	_ = g.UpsertEntity(ctx, Entity{Name: "Neo4j", Scope: ScopeUser, ScopeID: "u", SourceMemoryID: "m1"})
	seeds, err := g.MatchSeeds(ctx, ScopeUser, "u", "tell me about neo4j please", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) != 1 {
		t.Fatalf("want 1 seed, got %#v", seeds)
	}
}
