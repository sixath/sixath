package middleware

import (
	"context"
	"testing"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
)

func BenchmarkChain_NoMiddleware(b *testing.B) {
	final := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return &agent.Response{Text: "ok"}, nil
	}
	h := Chain(final)
	req := &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = h(ctx, req)
	}
}

func BenchmarkChain_ThreeMiddlewares(b *testing.B) {
	final := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return &agent.Response{Text: "ok"}, nil
	}
	h := Chain(final,
		LoggingMiddleware,
		RecoveryMiddleware,
		MetricsMiddleware,
	)
	req := &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = h(ctx, req)
	}
}
