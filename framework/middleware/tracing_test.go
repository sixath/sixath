package middleware

import (
	"context"
	"errors"
	"testing"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracingMiddleware_SuccessAttributes(t *testing.T) {
	sink := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(sink))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	otel.SetTracerProvider(tp)

	final := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return &agent.Response{
			Text: "hello",
			Metadata: map[string]any{
				"token_input":  float64(10),
				"token_output": float64(5),
			},
		}, nil
	}
	h := TracingMiddleware(final)
	req := &agent.Request{
		Messages:  []model.Message{{Role: "user", Content: "hi"}},
		RequestID: "req-abc",
		Metadata: map[string]any{
			"agent_name": "dataquery",
			"user_id":    "user-1",
			"model":      "test-model",
		},
	}
	_, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	spans := sink.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	s := spans[0]
	if s.Name != "Agent.Run/dataquery" {
		t.Errorf("span name = %q, want Agent.Run/dataquery", s.Name)
	}
	if s.Status.Code != codes.Ok {
		t.Errorf("status = %v, want Ok", s.Status.Code)
	}
	attrs := attrMap(s.Attributes)
	for _, key := range []string{
		"agent.name", "agent.messages_count", "agent.request_id",
		"enduser.id", "agent.model", "agent.response_length",
		"agent.token.input", "agent.token.output",
	} {
		if _, ok := attrs[key]; !ok {
			t.Errorf("missing attribute %q", key)
		}
	}
}

func TestTracingMiddleware_ErrorStatus(t *testing.T) {
	sink := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(sink))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	otel.SetTracerProvider(tp)

	wantErr := errors.New("boom")
	h := TracingMiddleware(func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return nil, wantErr
	})
	_, err := h(context.Background(), &agent.Request{Metadata: map[string]any{"agent_name": "x"}})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v", err)
	}
	spans := sink.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Errorf("status = %v, want Error", spans[0].Status.Code)
	}
	if spans[0].Name != "Agent.Run/x" {
		t.Errorf("span name = %q", spans[0].Name)
	}
}

func attrMap(attrs []attribute.KeyValue) map[string]any {
	m := make(map[string]any, len(attrs))
	for _, a := range attrs {
		m[string(a.Key)] = a.Value.AsInterface()
	}
	return m
}
