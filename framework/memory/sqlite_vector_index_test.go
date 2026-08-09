package memory

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteVectorIndex_UpsertSearchDelete(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewSQLiteVectorIndex(filepath.Join(dir, "units.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	ctx := context.Background()
	a := []float32{1, 0, 0}
	b := []float32{0.9, 0.1, 0}
	c := []float32{0, 1, 0}

	if err := idx.Upsert(ctx, UnitVectorRecord{UnitID: "u1", Scope: ScopeSession, ScopeID: "s1", Embedding: a}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(ctx, UnitVectorRecord{UnitID: "u2", Scope: ScopeSession, ScopeID: "s1", Embedding: b}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(ctx, UnitVectorRecord{UnitID: "u3", Scope: ScopeSession, ScopeID: "s2", Embedding: c}); err != nil {
		t.Fatal(err)
	}

	hits, err := idx.Search(ctx, VectorSearchQuery{Scope: ScopeSession, ScopeID: "s1", Embedding: a, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("want 2 hits, got %#v", hits)
	}
	if hits[0].UnitID != "u1" {
		t.Fatalf("top should be u1, got %#v", hits)
	}
	// other scope must not appear
	for _, h := range hits {
		if h.UnitID == "u3" {
			t.Fatalf("leaked other scope: %#v", hits)
		}
	}

	if err := idx.Delete(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	hits, err = idx.Search(ctx, VectorSearchQuery{Scope: ScopeSession, ScopeID: "s1", Embedding: a, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.UnitID == "u1" {
			t.Fatalf("deleted u1 still returned: %#v", hits)
		}
	}
}

func TestSQLiteVectorIndex_UpsertReplaces(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewSQLiteVectorIndex(filepath.Join(dir, "units.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ctx := context.Background()
	if err := idx.Upsert(ctx, UnitVectorRecord{UnitID: "u1", Scope: ScopeUser, ScopeID: "u", Embedding: []float32{1, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(ctx, UnitVectorRecord{UnitID: "u1", Scope: ScopeUser, ScopeID: "u", Embedding: []float32{0, 1}}); err != nil {
		t.Fatal(err)
	}
	hits, err := idx.Search(ctx, VectorSearchQuery{Scope: ScopeUser, ScopeID: "u", Embedding: []float32{0, 1}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].UnitID != "u1" {
		t.Fatalf("got %#v", hits)
	}
	if hits[0].Score < 0.99 {
		t.Fatalf("expected near 1 after replace, score=%v", hits[0].Score)
	}
}
