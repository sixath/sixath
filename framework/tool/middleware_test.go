package tool

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/sixath/framework/events"
)

func newTestRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
}

func TestMiddleware_AliasesBeforeSchema(t *testing.T) {
	r := newTestRegistry()
	var got map[string]any
	err := r.Register(Tool{
		Name:       "q",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"dsl": map[string]any{"type": "string"}}, "required": []any{"dsl"}},
		ArgAliases: map[string]string{"query": "dsl"},
		Execute:    func(_ context.Context, p map[string]any) (any, error) { got = p; return "ok", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	tl, _ := r.Get("q")
	if _, err := tl.Execute(context.Background(), map[string]any{"query": "SELECT 1"}); err != nil {
		t.Fatalf("alias must satisfy required: %v", err)
	}
	if got["dsl"] != "SELECT 1" || got["query"] != nil {
		t.Fatalf("got %v", got)
	}
	_, _ = tl.Execute(context.Background(), map[string]any{"dsl": "A", "query": "B"})
	if got["dsl"] != "A" {
		t.Fatalf("alias must not override canonical: %v", got)
	}
}

func TestMiddleware_ChecksRejectAndAggregate(t *testing.T) {
	r := newTestRegistry()
	executed := false
	src := func(context.Context, map[string]any) ([]string, error) { return []string{"a1", "b1"}, nil }
	_ = r.Register(Tool{
		Name:      "t",
		ArgChecks: []ArgCheck{OneOf{Param: "x", Source: src}, OneOf{Param: "y", Source: src}},
		Execute:   func(context.Context, map[string]any) (any, error) { executed = true; return "ok", nil },
	})
	tl, _ := r.Get("t")
	res, err := tl.Execute(context.Background(), map[string]any{"x": "a2", "y": "b2"})
	var iae *InvalidArgumentsError
	if !errors.As(err, &iae) || len(iae.Errors) != 2 || executed {
		t.Fatalf("err=%v executed=%v", err, executed)
	}
	m := res.(map[string]any)
	if m["ok"] != false || m["error_code"] != ErrorPermanent || m["invalid_arguments"] == nil {
		t.Fatalf("result %+v", m)
	}
}

func TestMiddleware_CheckErrorFailsOpenAndMarksSuspectOnEmpty(t *testing.T) {
	r := newTestRegistry()
	_ = r.Register(Tool{
		Name: "t",
		ArgChecks: []ArgCheck{OneOf{Param: "x", Source: func(context.Context, map[string]any) ([]string, error) {
			return nil, errors.New("catalog down")
		}}},
		Execute: func(context.Context, map[string]any) (any, error) {
			return map[string]any{"ok": true, "hit_status": HitStatusEmpty}, nil
		},
	})
	tl, _ := r.Get("t")
	res, err := tl.Execute(context.Background(), map[string]any{"x": "zz"})
	if err != nil {
		t.Fatalf("fail-open expected: %v", err)
	}
	m := res.(map[string]any)
	if m["hit_status"] != HitStatusSuspect || m["check_skipped"] == nil {
		t.Fatalf("got %+v", m)
	}
}

func TestMiddleware_ValidationEnvReadPerCall(t *testing.T) {
	r := newTestRegistry()
	_ = r.Register(Tool{
		Name:       "t",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}, "required": []any{"a"}},
		Execute:    func(context.Context, map[string]any) (any, error) { return "ok", nil },
	})
	tl, _ := r.Get("t")
	if _, err := tl.Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatal("expected rejection")
	}
	t.Setenv(EnvToolArgValidation, "off")
	if _, err := tl.Execute(context.Background(), map[string]any{}); err != nil {
		t.Fatalf("env off after Register must disable validation: %v", err)
	}
}

func TestMiddleware_RejectedCallsEmitEvents(t *testing.T) {
	bus := events.NewBus()
	var executed int32
	bus.Subscribe(false, func(_ context.Context, e events.Event) {
		if e.Kind == events.ToolExecuted {
			atomic.AddInt32(&executed, 1)
		}
	})
	r := newTestRegistry()
	r.eventBus = bus
	_ = r.Register(Tool{
		Name:       "t",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}, "required": []any{"a"}},
		Execute:    func(context.Context, map[string]any) (any, error) { return "ok", nil },
	})
	tl, _ := r.Get("t")
	_, _ = tl.Execute(context.Background(), map[string]any{})
	if atomic.LoadInt32(&executed) != 1 {
		t.Fatalf("rejected call must publish ToolExecuted, got %d", executed)
	}
}

func TestMiddleware_NotWrappedTwice(t *testing.T) {
	var probes int32
	parent := newTestRegistry()
	_ = parent.Register(Tool{
		Name: "t",
		EmptyProbe: &EmptyProbe{
			Relax: func(map[string]any) []ProbeVariant { return []ProbeVariant{{Label: "a"}} },
			Count: func(context.Context, ProbeVariant) (int64, error) { atomic.AddInt32(&probes, 1); return 0, nil },
		},
		Execute: func(context.Context, map[string]any) (any, error) {
			return map[string]any{"ok": true, "hit_status": HitStatusEmpty}, nil
		},
	})
	tl, _ := parent.Get("t")
	sub := newTestRegistry()
	if err := sub.Register(tl); err != nil {
		t.Fatal(err)
	}
	st, _ := sub.Get("t")
	_, _ = st.Execute(context.Background(), map[string]any{})
	if probes != 1 {
		t.Fatalf("probe ran %d times", probes)
	}
}
