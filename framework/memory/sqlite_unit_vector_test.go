package memory

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSQLiteUnitVectorIndex_PersistAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "uv.db")

	idx, err := NewSQLiteUnitVectorIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s", UnitID: "a", Vector: []float32{1, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil { // ??
		t.Fatalf("Close must be idempotent: %v", err)
	}

	reopened, err := NewSQLiteUnitVectorIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	hits, err := reopened.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0, 0}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].UnitID != "a" {
		t.Fatalf("not persisted: %+v", hits)
	}
	if hits[0].Score < 0.99 {
		t.Fatalf("cosine decode broken: %+v", hits)
	}
}

// ????????????????????????? Upsert ???
func TestSQLiteUnitVectorIndex_DimsBaselineAfterReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "uv.db")

	idx, _ := NewSQLiteUnitVectorIndex(path)
	_ = idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s", UnitID: "a", Vector: []float32{1, 0, 0}})
	_ = idx.Close()

	reopened, _ := NewSQLiteUnitVectorIndex(path)
	defer reopened.Close()

	if err := reopened.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s", UnitID: "b", Vector: []float32{1, 0}}); err == nil {
		t.Fatal("want dimension mismatch error after reopen")
	}
}

func TestSQLiteUnitVectorIndex_DimensionMismatch(t *testing.T) {
	ctx := context.Background()
	idx, _ := NewSQLiteUnitVectorIndex(filepath.Join(t.TempDir(), "uv.db"))
	defer idx.Close()

	if err := idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s", UnitID: "a", Vector: []float32{1, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s", UnitID: "b", Vector: []float32{1, 0}}); err == nil {
		t.Fatal("want upsert dimension error")
	} else if !errors.Is(err, ErrVectorDimMismatch) {
		t.Fatalf("want ErrVectorDimMismatch, got %v", err)
	}
	if _, err := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 3}); err == nil {
		t.Fatal("want search dimension error")
	} else if !errors.Is(err, ErrVectorDimMismatch) {
		t.Fatalf("want ErrVectorDimMismatch on search, got %v", err)
	}
}

func TestSQLiteUnitVectorIndex_Has(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "uv.db")
	idx, err := NewSQLiteUnitVectorIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s1", UnitID: "a", Vector: []float32{1, 0}})
	_ = idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeUser, ScopeID: "u1", UnitID: "a", Vector: []float32{1, 0}})

	got, err := idx.Has(ctx, ScopeSession, "s1", []string{"a", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if !got["a"] || got["missing"] {
		t.Fatalf("got %+v", got)
	}
	empty, err := idx.Has(ctx, ScopeSession, "s1", nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty: %+v err=%v", empty, err)
	}
	_ = idx.Close()

	reopened, err := NewSQLiteUnitVectorIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got2, err := reopened.Has(ctx, ScopeSession, "s1", []string{"a"})
	if err != nil || !got2["a"] {
		t.Fatalf("after reopen: %+v err=%v", got2, err)
	}
}

func TestSQLiteUnitVectorIndex_ScopeIsolationAndDelete(t *testing.T) {
	ctx := context.Background()
	idx, _ := NewSQLiteUnitVectorIndex(filepath.Join(t.TempDir(), "uv.db"))
	defer idx.Close()

	_ = idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s1", UnitID: "a", Vector: []float32{1, 0}})
	_ = idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeUser, ScopeID: "s1", UnitID: "a", Vector: []float32{1, 0}})

	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 5})
	if len(hits) != 1 {
		t.Fatalf("scope leak: %+v", hits)
	}
	_ = idx.Delete(ctx, ScopeSession, "s1", "a")
	hits, _ = idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 5})
	if len(hits) != 0 {
		t.Fatalf("delete failed: %+v", hits)
	}
	hits, _ = idx.Search(ctx, UnitVectorQuery{Scope: ScopeUser, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 5})
	if len(hits) != 1 {
		t.Fatalf("user scope must survive: %+v", hits)
	}
}

func TestSQLiteUnitVectorIndex_CorruptEmbedding(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "uv.db")
	idx, err := NewSQLiteUnitVectorIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	if err := idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s", UnitID: "a", Vector: []float32{1, 0}}); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE unit_vectors SET embedding=? WHERE unit_id=?`, []byte{1, 2, 3}, "a"); err != nil {
		t.Fatal(err)
	}

	if _, err := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 5}); err == nil {
		t.Fatal("want corrupt embedding error")
	}
}

// Failed first Upsert must not pin the in-process dimension baseline.
func TestSQLiteUnitVectorIndex_FailedUpsertDoesNotPinDims(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uv.db")
	idx, err := NewSQLiteUnitVectorIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := idx.Upsert(canceled, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s", UnitID: "a", Vector: []float32{1, 0, 0}}); err == nil {
		t.Fatal("want cancelled upsert to fail")
	}

	if err := idx.Upsert(context.Background(), UnitVectorEntry{Scope: ScopeSession, ScopeID: "s", UnitID: "b", Vector: []float32{1, 0}}); err != nil {
		t.Fatalf("dims pinned by failed upsert: %v", err)
	}
}
