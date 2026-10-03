package tool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type countingMapper struct {
	fields []string
	calls  int
}

func (m *countingMapper) Lookup(context.Context, string, string) (ESFieldMapping, bool) {
	return ESFieldMapping{}, false
}
func (m *countingMapper) ListFields(context.Context, string) []string { m.calls++; return m.fields }

func TestCachedFieldMapper_TTLAndRefresh(t *testing.T) {
	inner := &countingMapper{fields: []string{"service"}}
	now := time.Unix(0, 0)
	c := newCachedFieldMapper(inner, 5*time.Minute)
	c.now = func() time.Time { return now }
	c.minRefresh = 0
	ctx := context.Background()

	c.ListFields(ctx, "logs-*")
	c.ListFields(ctx, "logs-*")
	if inner.calls != 1 {
		t.Fatalf("cached: calls=%d", inner.calls)
	}
	inner.fields = []string{"service", "new_field"}
	if got := c.Refresh(ctx, "logs-*"); len(got) != 2 || inner.calls != 2 {
		t.Fatalf("refresh: %v calls=%d", got, inner.calls)
	}
	now = now.Add(6 * time.Minute)
	c.ListFields(ctx, "logs-*")
	if inner.calls != 3 {
		t.Fatalf("expired: calls=%d", inner.calls)
	}
	inner.fields = nil
	c.Refresh(ctx, "other")
	c.ListFields(ctx, "other")
	if inner.calls != 5 {
		t.Fatalf("empty results must not be cached: calls=%d", inner.calls)
	}
}

func TestCachedFieldMapper_RefreshRateLimited(t *testing.T) {
	inner := &countingMapper{fields: []string{"a"}}
	now := time.Unix(0, 0)
	c := newCachedFieldMapper(inner, 5*time.Minute)
	c.now = func() time.Time { return now }
	ctx := context.Background()

	c.ListFields(ctx, "i")
	inner.fields = []string{"a", "b"}
	if got := c.Refresh(ctx, "i"); len(got) != 1 || inner.calls != 1 {
		t.Fatalf("refresh within window must return cache: %v calls=%d", got, inner.calls)
	}
	now = now.Add(31 * time.Second)
	if got := c.Refresh(ctx, "i"); len(got) != 2 || inner.calls != 2 {
		t.Fatalf("refresh after window must fetch: %v calls=%d", got, inner.calls)
	}
}

func TestCachedFieldMapper_CapEvictsOldest(t *testing.T) {
	inner := &countingMapper{fields: []string{"a"}}
	now := time.Unix(0, 0)
	c := newCachedFieldMapper(inner, 5*time.Minute)
	c.now = func() time.Time { return now }
	c.maxEntries = 2
	ctx := context.Background()
	for _, idx := range []string{"x", "y", "z"} {
		c.ListFields(ctx, idx)
		now = now.Add(time.Second)
	}
	if len(c.lists) != 2 {
		t.Fatalf("cap exceeded: %d", len(c.lists))
	}
	if _, ok := c.lists["x"]; ok {
		t.Fatal("oldest entry must be evicted")
	}
}

type blockingMapper struct {
	calls   atomic.Int32
	release chan struct{}
}

func (m *blockingMapper) Lookup(context.Context, string, string) (ESFieldMapping, bool) {
	return ESFieldMapping{}, false
}
func (m *blockingMapper) ListFields(context.Context, string) []string {
	m.calls.Add(1)
	<-m.release
	return []string{"a"}
}

func TestCachedFieldMapper_CoalescesConcurrentMisses(t *testing.T) {
	inner := &blockingMapper{release: make(chan struct{})}
	c := newCachedFieldMapper(inner, time.Minute)
	var wg sync.WaitGroup
	results := make([][]string, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = c.ListFields(context.Background(), "i")
		}(i)
	}
	for inner.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(inner.release)
	wg.Wait()
	if n := inner.calls.Load(); n != 1 {
		t.Fatalf("concurrent misses must coalesce, calls=%d", n)
	}
	for _, r := range results {
		if len(r) != 1 {
			t.Fatalf("results %v", results)
		}
	}
}

func TestESMapperCache_PerCluster(t *testing.T) {
	made := map[string]int{}
	mc := newESMapperCache(nil, func(cluster string) ESFieldMapper {
		made[cluster]++
		return &countingMapper{fields: []string{cluster}}
	}, time.Minute)
	if mc.For("a") != mc.For("a") || made["a"] != 1 {
		t.Fatalf("must reuse per cluster: %v", made)
	}
	if got := mc.For("b").ListFields(context.Background(), "x"); got[0] != "b" {
		t.Fatalf("got %v", got)
	}
}

func TestESMapperCache_NilMapperIsNil(t *testing.T) {
	mc := newESMapperCache(nil, func(string) ESFieldMapper { return nil }, time.Minute)
	if m := mc.For("a"); m != nil {
		t.Fatalf("want nil, got %v", m)
	}
}
