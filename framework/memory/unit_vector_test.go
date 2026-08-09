package memory

import (
	"context"
	"sync"
	"testing"
)

func TestInMemoryUnitVectorIndex_UpsertSearchDelete(t *testing.T) {
	ctx := context.Background()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
	}
	must(idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s1", UnitID: "a", Vector: []float32{1, 0}}))
	must(idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s1", UnitID: "b", Vector: []float32{0, 1}}))

	hits, err := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 2})
	must(err)
	if len(hits) != 2 || hits[0].UnitID != "a" {
		t.Fatalf("want a first, got %+v", hits)
	}
	if hits[0].Score <= hits[1].Score {
		t.Fatalf("scores must be descending: %+v", hits)
	}

	// Upsert 覆盖同键，不新增行，且向量确实被替换
	must(idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s1", UnitID: "a", Vector: []float32{0, 1}}))
	hits, err = idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 10})
	must(err)
	if len(hits) != 2 {
		t.Fatalf("upsert must overwrite, got %d", len(hits))
	}
	// "a" 被覆盖为 {0,1}，对 {1,0} 的相似度应接近 0，且不再高于 "b"
	byID := map[string]float64{}
	for _, h := range hits {
		byID[h.UnitID] = h.Score
	}
	if byID["a"] > 0.001 {
		t.Fatalf("upsert did not replace vector: a score=%v want ~0", byID["a"])
	}
	if byID["a"] > byID["b"] {
		t.Fatalf("overwritten a must not score above b: %+v", hits)
	}

	must(idx.Delete(ctx, ScopeSession, "s1", "a"))
	hits, err = idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 10})
	must(err)
	if len(hits) != 1 || hits[0].UnitID != "b" {
		t.Fatalf("delete failed: %+v", hits)
	}
	must(idx.Delete(ctx, ScopeSession, "s1")) // 空 id 列表 no-op
	hits, err = idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 10})
	must(err)
	if len(hits) != 1 || hits[0].UnitID != "b" {
		t.Fatalf("empty-id delete must be no-op, got %+v", hits)
	}
}

func TestInMemoryUnitVectorIndex_Has(t *testing.T) {
	ctx := context.Background()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	_ = idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s1", UnitID: "a", Vector: []float32{1}})
	_ = idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeUser, ScopeID: "u1", UnitID: "a", Vector: []float32{1}})

	got, err := idx.Has(ctx, ScopeSession, "s1", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !got["a"] || got["b"] {
		t.Fatalf("got %+v", got)
	}
	// scope isolation: session s1 must not see user u1's "a"
	gotUser, err := idx.Has(ctx, ScopeUser, "u1", []string{"a"})
	if err != nil || !gotUser["a"] {
		t.Fatalf("user has: %+v err=%v", gotUser, err)
	}
	gotWrong, err := idx.Has(ctx, ScopeSession, "u1", []string{"a"})
	if err != nil || gotWrong["a"] {
		t.Fatalf("cross-scope must miss: %+v err=%v", gotWrong, err)
	}
	empty, err := idx.Has(ctx, ScopeSession, "s1", nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty ids: %+v err=%v", empty, err)
	}
}

func TestInMemoryUnitVectorIndex_ScopeIsolation(t *testing.T) {
	ctx := context.Background()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()

	_ = idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s1", UnitID: "a", Vector: []float32{1, 0}})
	_ = idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s2", UnitID: "b", Vector: []float32{1, 0}})
	_ = idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeUser, ScopeID: "s1", UnitID: "c", Vector: []float32{1, 0}})

	hits, err := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].UnitID != "a" {
		t.Fatalf("scope leak: %+v", hits)
	}
}

func TestInMemoryUnitVectorIndex_MinScoreAndLimit(t *testing.T) {
	ctx := context.Background()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	_ = idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s", UnitID: "a", Vector: []float32{1, 0}})
	_ = idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s", UnitID: "b", Vector: []float32{0, 1}})

	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 1})
	if len(hits) != 1 || hits[0].UnitID != "a" {
		t.Fatalf("limit ignored: %+v", hits)
	}
	hits, _ = idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 10, MinScore: 0.5})
	if len(hits) != 1 || hits[0].UnitID != "a" {
		t.Fatalf("min score ignored: %+v", hits)
	}

	// Limit <= 0 与空查询向量都必须返回 nil, nil（参考实现的可执行契约）
	hits, err := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 0})
	if err != nil || hits != nil {
		t.Fatalf("limit<=0 must return nil,nil, got %+v err=%v", hits, err)
	}
	hits, err = idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: nil, Limit: 10})
	if err != nil || hits != nil {
		t.Fatalf("empty query vector must return nil,nil, got %+v err=%v", hits, err)
	}
}

func TestInMemoryUnitVectorIndex_Concurrent(t *testing.T) {
	ctx := context.Background()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func(n int) { defer wg.Done(); _ = idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s", UnitID: string(rune('a' + n%26)), Vector: []float32{1, 0}}) }(i)
		go func() { defer wg.Done(); _, _ = idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 5}) }()
		go func(n int) { defer wg.Done(); _ = idx.Delete(ctx, ScopeSession, "s", string(rune('a'+n%26))) }(i)
	}
	wg.Wait()
}
