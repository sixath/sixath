package middleware

import (
	"context"
	"testing"

	agent "github.com/sixath/framework/harness"
)

func TestChain_OnionOrder(t *testing.T) {
	var trace []int
	mk := func(n int) Middleware {
		return func(next Handler) Handler {
			return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
				trace = append(trace, n)
				return next(ctx, req)
			}
		}
	}
	final := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		trace = append(trace, 0)
		return &agent.Response{}, nil
	}
	h := Chain(final, mk(1), mk(2), mk(3))
	_, _ = h(context.Background(), &agent.Request{})
	want := []int{1, 2, 3, 0}
	if len(trace) != len(want) {
		t.Fatalf("trace=%v want=%v", trace, want)
	}
	for i := range want {
		if trace[i] != want[i] {
			t.Fatalf("trace=%v want=%v", trace, want)
		}
	}
}

func TestChainBuilder_OrderSemantics(t *testing.T) {
	var trace []int
	mkMw := func(label int) Middleware {
		return func(next Handler) Handler {
			return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
				trace = append(trace, label)
				return next(ctx, req)
			}
		}
	}
	final := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		trace = append(trace, 0)
		return &agent.Response{}, nil
	}
	h := ChainBuilder(final,
		OrderedMiddleware{Order: 10, Mw: mkMw(10)},
		OrderedMiddleware{Order: 1, Mw: mkMw(1)},
		OrderedMiddleware{Order: 5, Mw: mkMw(5)},
	)
	_, _ = h(context.Background(), &agent.Request{})
	want := []int{10, 5, 1, 0}
	if len(trace) != len(want) {
		t.Fatalf("trace=%v want=%v", trace, want)
	}
	for i := range want {
		if trace[i] != want[i] {
			t.Fatalf("trace=%v want=%v", trace, want)
		}
	}
}

func TestChainBuilder_Empty(t *testing.T) {
	called := false
	final := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		called = true
		return &agent.Response{}, nil
	}
	h := ChainBuilder(final)
	_, _ = h(context.Background(), &agent.Request{})
	if !called {
		t.Fatal("expected final handler called")
	}
}

func TestMergeGlobalLocal_GlobalFirst(t *testing.T) {
	var trace []int
	mk := func(n int) Middleware {
		return func(next Handler) Handler {
			return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
				trace = append(trace, n)
				return next(ctx, req)
			}
		}
	}
	final := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		trace = append(trace, 0)
		return &agent.Response{}, nil
	}
	h := MergeGlobalLocal(final, []Middleware{mk(100)}, []Middleware{mk(200)})
	_, _ = h(context.Background(), &agent.Request{})
	want := []int{100, 200, 0}
	for i := range want {
		if trace[i] != want[i] {
			t.Fatalf("trace=%v want=%v", trace, want)
		}
	}
}
