package middleware

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/sixath/framework/agent"
)

func TestStreamChain_Order(t *testing.T) {
	var trace []int
	mk := func(n int) StreamMiddleware {
		return func(next StreamHandler) StreamHandler {
			return func(ctx context.Context, req *agent.Request) (<-chan agent.ResponseChunk, error) {
				trace = append(trace, n)
				return next(ctx, req)
			}
		}
	}
	final := func(ctx context.Context, req *agent.Request) (<-chan agent.ResponseChunk, error) {
		trace = append(trace, 0)
		return singleChunkChan("ok"), nil
	}
	h := StreamChain(final, mk(1), mk(2))
	ch, err := h(context.Background(), &agent.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DrainStringChunks(ch); err != nil {
		t.Fatal(err)
	}
	want := []int{1, 2, 0}
	if len(trace) != len(want) {
		t.Fatalf("trace=%v want=%v", trace, want)
	}
	for i := range want {
		if trace[i] != want[i] {
			t.Fatalf("trace=%v want=%v", trace, want)
		}
	}
}

func TestStreamCache_HitReplay(t *testing.T) {
	store := NewCacheStore(0)
	store.Set((&DefaultCacheKey{Version: 1}).BuildKey(&agent.Request{}), &agent.Response{Text: "cached"})
	var calls atomic.Int32
	h := StreamCacheMiddleware(store, nil)(func(ctx context.Context, req *agent.Request) (<-chan agent.ResponseChunk, error) {
		calls.Add(1)
		return singleChunkChan("live"), nil
	})
	ch, err := h(context.Background(), &agent.Request{})
	if err != nil {
		t.Fatal(err)
	}
	text, err := DrainStringChunks(ch)
	if err != nil || text != "cached" || calls.Load() != 0 {
		t.Fatalf("text=%q err=%v calls=%d", text, err, calls.Load())
	}
}

func TestStreamCache_MissCollect(t *testing.T) {
	store := NewCacheStore(0)
	key := (&DefaultCacheKey{Version: 1}).BuildKey(&agent.Request{})
	h := StreamCacheMiddleware(store, nil)(func(ctx context.Context, req *agent.Request) (<-chan agent.ResponseChunk, error) {
		return singleChunkChan("streamed"), nil
	})
	ch, err := h(context.Background(), &agent.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DrainStringChunks(ch); err != nil {
		t.Fatal(err)
	}
	if got, ok := store.Get(key); !ok || got.Text != "streamed" {
		t.Fatalf("cache = %v ok=%v", got, ok)
	}
}
