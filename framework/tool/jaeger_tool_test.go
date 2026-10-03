package tool

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const jaegerTraceBody = `{"data":[{"traceID":"abc","spans":[
  {"spanID":"s1","operationName":"GET /x","duration":1200,"startTime":1710000000000000,"processID":"p1","tags":[{"key":"error","value":true}]},
  {"spanID":"s2","operationName":"db.query","duration":800,"startTime":1710000000500000,"processID":"p2","tags":[]}],
  "processes":{"p1":{"serviceName":"service-a"},"p2":{"serviceName":"service-b"}}}]}`

func TestJaegerTrace_ByTraceID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/traces/") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jaegerTraceBody))
	}))
	defer srv.Close()

	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	if err := RegisterJaegerTool(reg, srv.URL); err != nil {
		t.Fatalf("register: %v", err)
	}
	tl, ok := reg.Get("jaeger_trace")
	if !ok {
		t.Fatal("jaeger_trace not registered")
	}
	out, err := tl.Execute(context.Background(), map[string]any{"trace_id": "abcdef0123456789"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)

	spans := m["spans"].([]map[string]any)
	if len(spans) != 2 {
		t.Fatalf("want 2 spans, got %d", len(spans))
	}
	if spans[0]["service"].(string) != "service-a" || spans[0]["duration_ms"].(float64) != 1.2 {
		t.Fatalf("span0 mapping wrong: %v", spans[0])
	}

	errs := m["errors"].([]map[string]any)
	if len(errs) != 1 || errs[0]["service"].(string) != "service-a" {
		t.Fatalf("want 1 error span from service-a, got %v", errs)
	}

	services := m["services"].([]string)
	if len(services) != 2 {
		t.Fatalf("want 2 distinct services, got %v", services)
	}

	assertRCAEvidenceOK(t, m, "jaeger_trace")
	refs := m["evidence_refs"].([]EvidenceRef)
	if refs[0].TraceID != "abcdef0123456789" {
		t.Fatalf("evidence_refs[0].TraceID=%q, want abcdef0123456789", refs[0].TraceID)
	}
}

func TestJaegerTrace_StringErrorTag(t *testing.T) {
	body := `{"data":[{"traceID":"t","spans":[
	  {"spanID":"s1","operationName":"op","duration":100,"startTime":1,"processID":"p1","tags":[{"key":"error","value":"true"}]}],
	  "processes":{"p1":{"serviceName":"svc"}}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterJaegerTool(reg, srv.URL)
	tl, _ := reg.Get("jaeger_trace")
	out, _ := tl.Execute(context.Background(), map[string]any{"trace_id": "0123456789abcdef"})
	errs := out.(map[string]any)["errors"].([]map[string]any)
	if len(errs) != 1 {
		t.Fatalf("string 'true' error tag should be detected, got %d error spans", len(errs))
	}
}

func TestJaegerTrace_RequiresParam(t *testing.T) {
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterJaegerTool(reg, "http://unused")
	tl, _ := reg.Get("jaeger_trace")
	out, _ := tl.Execute(context.Background(), map[string]any{})
	m := out.(map[string]any)
	if _, has := m["error"]; !has {
		t.Fatal("expected error when neither trace_id nor service provided")
	}
	assertRCAEvidenceError(t, m, ErrorPermanent)
}

func TestJaegerTrace_TimeoutTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterJaegerTool(reg, srv.URL)
	tl, _ := reg.Get("jaeger_trace")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	out, err := tl.Execute(ctx, map[string]any{"trace_id": "abcdef0123456789"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	assertRCAEvidenceError(t, out.(map[string]any), ErrorTransient)
}

func TestJaegerTrace_HTTP5xxTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream down"))
	}))
	defer srv.Close()

	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterJaegerTool(reg, srv.URL)
	tl, _ := reg.Get("jaeger_trace")
	out, err := tl.Execute(context.Background(), map[string]any{"trace_id": "abcdef0123456789"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	assertRCAEvidenceError(t, out.(map[string]any), ErrorTransient)
}

func TestJaegerTrace_HTTP4xxPermanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("trace not found"))
	}))
	defer srv.Close()

	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterJaegerTool(reg, srv.URL)
	tl, _ := reg.Get("jaeger_trace")
	out, err := tl.Execute(context.Background(), map[string]any{"trace_id": "ffffffffffffffff"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	assertRCAEvidenceError(t, out.(map[string]any), ErrorPermanent)
}

func TestJaegerTrace_RejectsMalformedTraceID(t *testing.T) {
	reg := newTestRegistry()
	if err := RegisterJaegerTool(reg, "http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("jaeger_trace")
	_, err := tl.Execute(context.Background(), map[string]any{"trace_id": "req-123"})
	var iae *InvalidArgumentsError
	if !errors.As(err, &iae) || iae.Errors[0].Keyword != KeywordPattern {
		t.Fatalf("got %v", err)
	}
	for _, bad := range []string{"0123456789abcdef0123456789abcdef0", "xyz"} {
		if errs, _ := (Pattern{Param: "trace_id", Regex: jaegerTraceIDPattern}).Check(context.Background(), map[string]any{"trace_id": bad}); len(errs) != 1 {
			t.Fatalf("%q must be rejected", bad)
		}
	}
	for _, ok := range []string{"0123456789abcdef", "0123456789ABCDEF0123456789abcdef", "123456789abcdef", "123456789abcdef0123456789abcdef"} {
		if errs, _ := (Pattern{Param: "trace_id", Regex: jaegerTraceIDPattern}).Check(context.Background(), map[string]any{"trace_id": ok}); len(errs) != 0 {
			t.Fatalf("%q must be accepted: %v", ok, errs)
		}
	}
}

func TestJaegerTrace_EmptyDataOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterJaegerTool(reg, srv.URL)
	tl, _ := reg.Get("jaeger_trace")
	out, err := tl.Execute(context.Background(), map[string]any{"trace_id": "abcdef0123456789"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	assertRCAEvidenceOK(t, m, "jaeger_trace")
}
