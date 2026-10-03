package tool

import (
	"context"
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
