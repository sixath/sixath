package tool

import (
	"context"
	"errors"
	"testing"
)

func TestBuildToolCatalog_AvailableOnly(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(Tool{
		Name: "alpha", Description: "does alpha", Toolset: ToolsetWeb,
		Execute: func(ctx context.Context, params map[string]any) (any, error) { return "ok", nil },
	})
	_ = reg.Register(Tool{
		Name: "gated", Description: "gated", Toolset: ToolsetWeb,
		CheckFn: func(ctx context.Context) error { return errors.New("no key") },
		Execute: func(ctx context.Context, params map[string]any) (any, error) { return nil, nil },
	})
	cat := BuildToolCatalog(context.Background(), reg)
	var alphaFound, gatedFound bool
	for _, e := range cat.Entries {
		if e.Name == "alpha" {
			alphaFound = true
			if !e.Available {
				t.Fatal("alpha should be available")
			}
		}
		if e.Name == "gated" {
			gatedFound = true
		}
	}
	if !alphaFound {
		t.Fatal("alpha should be in catalog")
	}
	if gatedFound {
		t.Fatal("gated tool should be excluded from catalog")
	}
}

func TestBuildToolCatalog_BuiltinHints(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(Tool{
		Name: "web_search", Description: "search web", Toolset: ToolsetWeb,
		Execute: func(ctx context.Context, params map[string]any) (any, error) { return nil, nil },
	})
	cat := BuildToolCatalog(context.Background(), reg, &BuiltinHintProvider{})
	found := false
	for _, e := range cat.Entries {
		if e.Name == "web_search" && len(e.SearchHints) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("web_search should have builtin search hints")
	}
}

func TestBuildToolCatalog_DeferredFlag(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(Tool{
		Name: "web_search", Description: "search web", Toolset: ToolsetWeb,
		Execute: func(ctx context.Context, params map[string]any) (any, error) { return nil, nil },
	})
	_ = reg.Register(Tool{
		Name: "execute_read", Description: "read rows", Toolset: ToolsetFile,
		Execute: func(ctx context.Context, params map[string]any) (any, error) { return nil, nil },
	})
	cat := BuildToolCatalog(context.Background(), reg)
	var webDeferred, fileDeferred bool
	for _, e := range cat.Entries {
		switch e.Name {
		case "web_search":
			webDeferred = e.Deferred
		case "execute_read":
			fileDeferred = e.Deferred
		}
	}
	if !webDeferred {
		t.Fatal("web_search should be deferred")
	}
	if fileDeferred {
		t.Fatal("execute_read should not be deferred")
	}
}
