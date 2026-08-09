package chat

import (
	"context"
	"os"
	"testing"

	"github.com/sixath/framework/tool"
)

func TestRegisterToolSearchIfNeeded_OnWithDeferred(t *testing.T) {
	t.Setenv("SATH_TOOL_SEARCH", "on")

	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:        "mcp__test__deferred",
		Description: "deferred MCP tool for wiring test",
		Parameters:  map[string]any{"type": "object"},
		Toolset:     tool.ToolsetMCP,
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	_ = RegisterCatalogTools(reg)
	catalog := BuildCatalogForAgent(context.Background(), CatalogWiringInput{Reg: reg})

	active, err := RegisterToolSearchIfNeeded(context.Background(), reg, catalog)
	if err != nil {
		t.Fatalf("RegisterToolSearchIfNeeded: %v", err)
	}
	if !active {
		t.Fatal("expected tool_search active with SATH_TOOL_SEARCH=on and deferred tool")
	}
	if _, ok := reg.Get(tool.ToolSearchName); !ok {
		t.Fatal("tool_search not registered")
	}
}

func TestRegisterToolSearchIfNeeded_Off(t *testing.T) {
	t.Setenv("SATH_TOOL_SEARCH", "off")
	// Clear env after test in case other tests depend on default
	defer os.Unsetenv("SATH_TOOL_SEARCH")

	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:        "mcp__test__deferred",
		Description: "deferred MCP tool",
		Parameters:  map[string]any{"type": "object"},
		Toolset:     tool.ToolsetMCP,
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	catalog := BuildCatalogForAgent(context.Background(), CatalogWiringInput{Reg: reg})

	active, err := RegisterToolSearchIfNeeded(context.Background(), reg, catalog)
	if err != nil {
		t.Fatalf("RegisterToolSearchIfNeeded: %v", err)
	}
	if active {
		t.Fatal("expected tool_search inactive with SATH_TOOL_SEARCH=off")
	}
}
