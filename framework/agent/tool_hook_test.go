package agent

import (
	"context"
	"errors"
	"testing"
)

type recordingHook struct {
	name   string
	order  *[]string
	before func(ctx context.Context, toolName string, args map[string]any) (map[string]any, error)
	after  func(ctx context.Context, toolName string, result any, err error) (any, error)
}

func (h *recordingHook) Before(ctx context.Context, toolName string, args map[string]any) (map[string]any, error) {
	*h.order = append(*h.order, h.name+":before")
	if h.before != nil {
		return h.before(ctx, toolName, args)
	}
	return args, nil
}

func (h *recordingHook) After(ctx context.Context, toolName string, result any, err error) (any, error) {
	*h.order = append(*h.order, h.name+":after")
	if h.after != nil {
		return h.after(ctx, toolName, result, err)
	}
	return result, err
}

func TestRunToolHooksBefore_orderAndArgsChain(t *testing.T) {
	var order []string
	h1 := &recordingHook{name: "h1", order: &order, before: func(_ context.Context, _ string, args map[string]any) (map[string]any, error) {
		out := map[string]any{"x": 1}
		for k, v := range args {
			out[k] = v
		}
		out["from"] = "h1"
		return out, nil
	}}
	h2 := &recordingHook{name: "h2", order: &order, before: func(_ context.Context, _ string, args map[string]any) (map[string]any, error) {
		if args["from"] != "h1" {
			t.Fatalf("expected h1 chain, got %#v", args)
		}
		args["from"] = "h2"
		return args, nil
	}}
	out, err := runToolHooksBefore(context.Background(), []ToolHook{h1, h2}, "demo", map[string]any{"a": true})
	if err != nil {
		t.Fatal(err)
	}
	if out["from"] != "h2" {
		t.Fatalf("got %#v", out)
	}
	want := []string{"h1:before", "h2:before"}
	if len(order) != 2 || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("order=%v", order)
	}
}

func TestRunToolHooksBefore_block(t *testing.T) {
	h := &recordingHook{name: "deny", order: new([]string), before: func(context.Context, string, map[string]any) (map[string]any, error) {
		return nil, errors.New("not allowed by policy hook")
	}}
	_, err := runToolHooksBefore(context.Background(), []ToolHook{h}, "demo", map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrToolHookBlocked) {
		t.Fatalf("expected ErrToolHookBlocked, got %v", err)
	}
}

func TestRunToolHooksBefore_skipsNilHook(t *testing.T) {
	var order []string
	h1 := &recordingHook{name: "h1", order: &order, before: func(_ context.Context, _ string, args map[string]any) (map[string]any, error) {
		args["from"] = "h1"
		return args, nil
	}}
	h2 := &recordingHook{name: "h2", order: &order, before: func(_ context.Context, _ string, args map[string]any) (map[string]any, error) {
		if args["from"] != "h1" {
			t.Fatalf("expected h1 chain, got %#v", args)
		}
		args["from"] = "h2"
		return args, nil
	}}
	out, err := runToolHooksBefore(context.Background(), []ToolHook{h1, nil, h2}, "demo", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if out["from"] != "h2" {
		t.Fatalf("got %#v", out)
	}
	want := []string{"h1:before", "h2:before"}
	if len(order) != 2 || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("order=%v", order)
	}
}

func TestRunToolHooksBefore_nilParams(t *testing.T) {
	var order []string
	h := &recordingHook{name: "h1", order: &order, before: func(_ context.Context, _ string, args map[string]any) (map[string]any, error) {
		if args == nil {
			t.Fatal("expected non-nil params map")
		}
		return args, nil
	}}
	out, err := runToolHooksBefore(context.Background(), []ToolHook{h}, "demo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatal("expected non-nil out map")
	}
}

func TestRunToolHooksAfter_sameOrderAsBefore(t *testing.T) {
	var order []string
	h1 := &recordingHook{name: "h1", order: &order}
	h2 := &recordingHook{name: "h2", order: &order}
	_, err := runToolHooksAfter(context.Background(), []ToolHook{h1, h2}, "demo", "ok", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "h1:after" || order[1] != "h2:after" {
		t.Fatalf("After must be same order as Before, got %v", order)
	}
}
