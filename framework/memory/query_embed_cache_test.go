package memory

import (
	"fmt"
	"sync"
	"testing"
)

func TestQueryEmbedCache_Hit(t *testing.T) {
	c := newQueryEmbedCache(2)
	c.put("a\x00q", []float32{1, 2})
	got := c.get("a\x00q")
	if got == nil {
		t.Fatal("expected hit for a")
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("got %v want [1 2]", got)
	}
	if c.get("missing") != nil {
		t.Fatal("expected miss for missing key")
	}
}

func TestQueryEmbedCache_GetRefreshesLRU(t *testing.T) {
	c := newQueryEmbedCache(2)
	c.put("a\x00q", []float32{1})
	c.put("b\x00q", []float32{2})
	// Touch a so it becomes most-recent; b is LRU.
	if c.get("a\x00q") == nil {
		t.Fatal("expected hit for a")
	}
	c.put("c\x00q", []float32{3}) // should evict b
	if c.get("b\x00q") != nil {
		t.Fatal("expected b evicted after get(a) refresh")
	}
	if c.get("a\x00q") == nil {
		t.Fatal("expected a retained")
	}
	if c.get("c\x00q") == nil {
		t.Fatal("expected c present")
	}
}

func TestQueryEmbedCache_PutOverwriteRefreshes(t *testing.T) {
	c := newQueryEmbedCache(2)
	c.put("a\x00q", []float32{1})
	c.put("b\x00q", []float32{2})
	// Overwrite a — refreshes a to MRU; b becomes LRU.
	c.put("a\x00q", []float32{10})
	c.put("c\x00q", []float32{3}) // should evict b
	if c.get("b\x00q") != nil {
		t.Fatal("expected b evicted after put(a) overwrite refresh")
	}
	got := c.get("a\x00q")
	if got == nil || len(got) != 1 || got[0] != 10 {
		t.Fatalf("expected overwritten a=[10], got %v", got)
	}
	if c.get("c\x00q") == nil {
		t.Fatal("expected c present")
	}
}

func TestQueryEmbedCache_NonPositiveCapacityDisabled(t *testing.T) {
	for _, cap := range []int{0, -1} {
		c := newQueryEmbedCache(cap)
		c.put("k", []float32{1})
		if got := c.get("k"); got != nil {
			t.Fatalf("capacity=%d: put should be no-op, get=%v", cap, got)
		}
	}
}

func TestQueryEmbedCache_SliceIsolation(t *testing.T) {
	c := newQueryEmbedCache(4)
	in := []float32{1, 2, 3}
	c.put("k", in)
	in[0] = 99 // mutating input must not affect cache

	out := c.get("k")
	if out == nil || out[0] != 1 {
		t.Fatalf("cache must own a copy of put input, got %v", out)
	}
	out[1] = 77 // mutating get result must not affect cache
	out2 := c.get("k")
	if out2 == nil || out2[1] != 2 {
		t.Fatalf("get must return a defensive copy, got %v", out2)
	}
}

func TestQueryEmbedCache_Concurrent(t *testing.T) {
	c := newQueryEmbedCache(64)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("a\x00%d", i%8)
			c.put(key, []float32{float32(i)})
			_ = c.get(key)
		}(i)
	}
	wg.Wait()
}
