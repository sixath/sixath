package middleware

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
)

func TestCacheMiddleware_NilRequest(t *testing.T) {
	calls := 0
	h := CacheMiddleware(NewCacheStore(0))(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		calls++
		return &agent.Response{Text: "ok"}, nil
	})
	resp, err := h(context.Background(), nil)
	if err != nil || resp.Text != "ok" || calls != 1 {
		t.Fatalf("calls=%d resp=%v err=%v", calls, resp, err)
	}
}

func TestCacheMiddleware_NilStore(t *testing.T) {
	calls := 0
	h := CacheMiddleware(nil)(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		calls++
		return &agent.Response{Text: "ok"}, nil
	})
	_, _ = h(context.Background(), &agent.Request{})
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestRateLimitMiddleware_NilLimiter(t *testing.T) {
	h := RateLimitMiddleware(nil, nil)(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return &agent.Response{}, nil
	})
	_, err := h(context.Background(), &agent.Request{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRateLimiter_CapacityZero(t *testing.T) {
	lim := NewRateLimiter(0, 0)
	if lim.allow("k") {
		t.Fatal("capacity 0 should deny")
	}
}

func TestContentSafetyMiddleware_NilFilter(t *testing.T) {
	h := ContentSafetyMiddleware(nil)(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return &agent.Response{Text: "ok"}, nil
	})
	resp, err := h(context.Background(), &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "badword"}},
	})
	if err != nil || resp.Text != "ok" {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
}

func TestDebugMiddleware_ErrorIncludesStackInLog(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	expectErr := errors.New("fail")
	h := DebugMiddlewareWithLogger(logger, true)(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return nil, expectErr
	})
	_, _ = h(context.Background(), &agent.Request{RequestID: "r1"})
	if !bytes.Contains(buf.Bytes(), []byte("stack")) {
		t.Fatalf("log missing stack: %s", buf.String())
	}
}

func TestMetricsMiddleware_ErrorStatus(t *testing.T) {
	var status string
	old := observeAgentRequest
	observeAgentRequest = func(agent, st string, _ time.Duration) {
		status = st
	}
	defer func() { observeAgentRequest = old }()

	h := MetricsMiddleware(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return nil, errors.New("x")
	})
	_, _ = h(context.Background(), &agent.Request{Metadata: map[string]any{"agent_name": "a"}})
	if status != "error" {
		t.Fatalf("status = %q", status)
	}
}
