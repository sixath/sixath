package tool

import (
	"context"
	"testing"
)

func TestRegister_DefaultToolset(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry")
	}
	ht, ok := r.Get("http_request")
	if !ok {
		t.Fatal("missing http_request")
	}
	if ht.Toolset != ToolsetWeb {
		t.Fatalf("http_request toolset: got %q want %q", ht.Toolset, ToolsetWeb)
	}
}

func TestListByToolsets(t *testing.T) {
	r := NewRegistry()
	_ = RegisterCalculatorTool(r)

	all := r.List()
	if len(all) < 2 {
		t.Fatalf("want at least http + calculator, got %d", len(all))
	}

	webOnly := r.ListByToolsets([]string{ToolsetWeb})
	if len(webOnly) != 1 || webOnly[0].Name != "http_request" {
		t.Fatalf("web only: %+v", webOnly)
	}

	skills := r.ListByToolsets([]string{ToolsetSkills})
	found := false
	for _, x := range skills {
		if x.Name == "calculator_add" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("skills set should include calculator_add, got %+v", skills)
	}

	empty := r.ListByToolsets(nil)
	if len(empty) != len(all) {
		t.Fatalf("nil toolsets should list all: %d vs %d", len(empty), len(all))
	}
}

func TestListByToolsets_UncategorizedExcluded(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(Tool{
		Name:        "orphan_echo",
		Description: "x",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return "ok", nil
		},
		// 故意不设 Toolset 且不在 builtinDefaultToolset
	})
	full := r.List()
	filtered := r.ListByToolsets([]string{ToolsetWeb})
	var hasOrphan bool
	for _, x := range filtered {
		if x.Name == "orphan_echo" {
			hasOrphan = true
		}
	}
	if hasOrphan {
		t.Fatal("orphan should not appear in filtered list")
	}
	var inFull bool
	for _, x := range full {
		if x.Name == "orphan_echo" {
			inFull = true
		}
	}
	if !inFull {
		t.Fatal("orphan should still be in List()")
	}
}
