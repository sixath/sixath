package middleware

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/errs"
)

func TestRecoveryMiddleware_PanicToError(t *testing.T) {
	tests := []struct {
		name  string
		panic any
	}{
		{"string", "boom"},
		{"error", errors.New("boom")},
		{"runtime error", runtimeError("bad")},
		{"int", 42},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := RecoveryMiddleware(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
				panic(tt.panic)
			})
			_, err := h(context.Background(), &agent.Request{})
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, errs.ErrInternal) {
				t.Fatalf("expected ErrInternal, got %v", err)
			}
		})
	}
}

func TestRecoveryMiddleware_NoPanicPassesThrough(t *testing.T) {
	want := errors.New("plain")
	h := RecoveryMiddleware(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return nil, want
	})
	_, err := h(context.Background(), &agent.Request{})
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func TestRecoveryMiddlewareWithLogger_LogsPanic(t *testing.T) {
	h := RecoveryMiddlewareWithLogger(slog.Default())(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		panic("x")
	})
	_, err := h(context.Background(), &agent.Request{RequestID: "r1"})
	if !errors.Is(err, errs.ErrInternal) {
		t.Fatalf("got %v", err)
	}
}

type runtimeError string

func (runtimeError) RuntimeError() {}
func (e runtimeError) Error() string { return string(e) }
