package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
)

func TestLoggingMiddleware_StructuredFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := LoggingMiddlewareWithLogger(logger)(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return &agent.Response{Text: "ok"}, nil
	})
	req := &agent.Request{
		RequestID: "rid-1",
		Metadata:  map[string]any{"agent_name": "dataquery", "user_id": "u1"},
		Messages:  []model.Message{{Role: "user", Content: "hi"}},
	}
	_, err := h(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(buf.Bytes()) {
		t.Fatalf("invalid JSON log: %s", buf.String())
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["request_id"] != "rid-1" || m["agent"] != "dataquery" {
		t.Fatalf("fields: %v", m)
	}
}

func TestLoggingMiddleware_SafeEscape(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	bad := errors.New(`quote " and \ newline ` + "\n")
	h := LoggingMiddlewareWithLogger(logger)(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return nil, bad
	})
	_, _ = h(context.Background(), &agent.Request{})
	if !json.Valid(buf.Bytes()) {
		t.Fatalf("invalid JSON with special err: %s", buf.String())
	}
}
