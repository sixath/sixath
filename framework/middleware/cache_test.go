package middleware

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
)

func TestCacheStore_TTLEvict(t *testing.T) {
	store := NewCacheStore(40*time.Millisecond, WithCacheMaxEntries(100))
	store.Set("k", &agent.Response{Text: "v"})
	if _, ok := store.Get("k"); !ok {
		t.Fatal("expected hit before TTL")
	}
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, ok := store.Get("k"); !ok {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("expected miss after TTL, len=%d", store.Len())
}

func TestCacheStore_LRUEvict(t *testing.T) {
	store := NewCacheStore(time.Hour, WithCacheMaxEntries(2))
	store.Set("a", &agent.Response{Text: "a"})
	store.Set("b", &agent.Response{Text: "b"})
	store.Set("c", &agent.Response{Text: "c"})
	if _, ok := store.Get("a"); ok {
		t.Fatal("expected a evicted")
	}
	if _, ok := store.Get("b"); !ok {
		t.Fatal("expected b present")
	}
	if _, ok := store.Get("c"); !ok {
		t.Fatal("expected c present")
	}
}

func TestCacheStore_Concurrent(t *testing.T) {
	store := NewCacheStore(time.Minute, WithCacheMaxEntries(50))
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := string(rune('a' + (n % 26)))
			store.Set(key, &agent.Response{Text: key})
			store.Get(key)
		}(i)
	}
	wg.Wait()
}

func TestCacheMiddleware_Singleflight(t *testing.T) {
	store := NewCacheStore(time.Minute)
	var calls atomic.Int32
	slow := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		calls.Add(1)
		time.Sleep(200 * time.Millisecond)
		return &agent.Response{Text: "ok"}, nil
	}
	h := CacheMiddleware(store)(slow)
	req := &agent.Request{Metadata: map[string]any{"agent_name": "sf-test"}}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := h(context.Background(), req)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
				return
			}
			if resp == nil || resp.Text != "ok" {
				t.Errorf("unexpected resp: %v", resp)
			}
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
}

func TestCacheMiddleware_DifferentKeysParallel(t *testing.T) {
	store := NewCacheStore(time.Minute)
	var calls atomic.Int32
	h := CacheMiddleware(store)(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		calls.Add(1)
		return &agent.Response{Text: req.Messages[0].Content}, nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			req := &agent.Request{
				Messages: []model.Message{{Role: "user", Content: fmt.Sprintf("msg-%d", n)}},
			}
			_, err := h(context.Background(), req)
			if err != nil {
				t.Errorf("err: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if got := calls.Load(); got != 10 {
		t.Fatalf("handler calls = %d, want 10", got)
	}
}
