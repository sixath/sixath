package middleware

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sixath/framework/agent"
)

func TestChain_NoMiddleware(t *testing.T) {
	called := false
	final := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		called = true
		return &agent.Response{Text: "ok"}, nil
	}
	h := Chain(final)
	resp, err := h(context.Background(), &agent.Request{})
	if err != nil || resp.Text != "ok" || !called {
		t.Fatalf("called=%v resp=%v err=%v", called, resp, err)
	}
}

func TestChain_ConcurrentSafe(t *testing.T) {
	var n atomic.Int32
	inc := func(next Handler) Handler {
		return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
			n.Add(1)
			return next(ctx, req)
		}
	}
	final := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return &agent.Response{}, nil
	}
	h := Chain(final, inc, inc)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = h(context.Background(), &agent.Request{})
		}()
	}
	wg.Wait()
	if n.Load() != 100 {
		t.Fatalf("middleware invocations = %d, want 100", n.Load())
	}
}
