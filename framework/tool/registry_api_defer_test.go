package tool

import (
	"context"
	"testing"
)

func setupRegistryWithDeferred(t *testing.T) *Registry {
	t.Helper()
	reg := NewRegistry()
	if reg == nil {
		t.Fatal("NewRegistry")
	}
	if err := reg.Register(Tool{
		Name:        "mcp__big__tool",
		Description: "large deferred MCP tool",
		Parameters:  map[string]any{"type": "object"},
		Toolset:     ToolsetMCP,
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	cat := BuildToolCatalog(context.Background(), reg)
	if err := RegisterToolSearchTools(reg, ToolSearchRegisterConfig{Registry: reg, Catalog: cat}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func containsToolName(list []Tool, name string) bool {
	for _, tl := range list {
		if tl.Name == name {
			return true
		}
	}
	return false
}

func TestListForAPIWithDefer_ExcludesDeferredWhenActive(t *testing.T) {
	reg := setupRegistryWithDeferred(t)
	ctx := context.WithValue(context.Background(), ContextKeyToolSearchActive, true)
	list := reg.ListForAPIWithDefer(ctx, nil, true)
	for _, tl := range list {
		if tl.Name == "mcp__big__tool" {
			t.Fatal("deferred tool should not appear when defer active")
		}
	}
	if !containsToolName(list, ToolSearchName) {
		t.Fatal("bridge tool tool_search should appear")
	}
}

func TestListForAPIWithDefer_InactiveReturnsAll(t *testing.T) {
	reg := setupRegistryWithDeferred(t)
	ctx := context.Background()
	withDefer := reg.ListForAPIWithDefer(ctx, nil, false)
	plain := reg.ListForAPI(ctx, nil)
	if len(withDefer) != len(plain) {
		t.Fatalf("inactive defer: got %d tools want %d (same as ListForAPI)", len(withDefer), len(plain))
	}
	namesWithDefer := make(map[string]struct{}, len(withDefer))
	for _, tl := range withDefer {
		namesWithDefer[tl.Name] = struct{}{}
	}
	for _, tl := range plain {
		if _, ok := namesWithDefer[tl.Name]; !ok {
			t.Fatalf("inactive defer missing tool %q from ListForAPI", tl.Name)
		}
	}
	if !containsToolName(withDefer, "mcp__big__tool") {
		t.Fatal("deferred tool should appear when defer inactive")
	}
}
