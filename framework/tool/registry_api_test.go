package tool

import (
	"context"
	"errors"
	"testing"
)

func TestListForAPI_ExcludesToolWhenCheckFnFails(t *testing.T) {
	reg := NewRegistry()
	if reg == nil {
		t.Fatal("NewRegistry")
	}
	if err := reg.Register(Tool{
		Name:        "gated_tool",
		Description: "test gated",
		Parameters:  map[string]any{"type": "object"},
		Toolset:     ToolsetWeb,
		CheckFn: func(ctx context.Context) error {
			return errors.New("missing api key")
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(Tool{
		Name:        "open_tool",
		Description: "always available",
		Parameters:  map[string]any{"type": "object"},
		Toolset:     ToolsetWeb,
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return "ok", nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	list := reg.ListForAPI(context.Background(), nil)
	for _, tl := range list {
		if tl.Name == "gated_tool" {
			t.Fatal("gated_tool should be excluded from API schema")
		}
	}
	foundOpen := false
	for _, tl := range list {
		if tl.Name == "open_tool" {
			foundOpen = true
		}
	}
	if !foundOpen {
		t.Fatal("open_tool should remain in API schema")
	}
}

func TestListForAPI_FiltersByToolsetsFromContext(t *testing.T) {
	reg := NewRegistry()
	if reg == nil {
		t.Fatal("NewRegistry")
	}
	if err := reg.Register(Tool{
		Name:        "demo_skill_tool",
		Description: "skills only",
		Parameters:  map[string]any{"type": "object"},
		Toolset:     ToolsetSkills,
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), ContextKeyEnabledToolsets, []string{ToolsetSkills})
	list := reg.ListForAPI(ctx, nil)
	for _, tl := range list {
		if tl.Toolset != ToolsetSkills {
			t.Fatalf("unexpected tool %q toolset %q", tl.Name, tl.Toolset)
		}
	}
	if len(list) == 0 {
		t.Fatal("expected at least one skills tool")
	}
}

func TestListForAPI_CheckFnPassesWhenNil(t *testing.T) {
	reg := NewRegistry()
	if reg == nil {
		t.Fatal("NewRegistry")
	}
	before := len(reg.ListForAPI(context.Background(), nil))
	if before == 0 {
		t.Fatal("expected default http_request")
	}
}

func TestEnabledToolsetsFromContext_EmptyWhenMissing(t *testing.T) {
	if got := EnabledToolsetsFromContext(context.Background()); got != nil {
		t.Fatalf("got %v want nil", got)
	}
}
