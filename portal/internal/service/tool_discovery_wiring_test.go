package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"backend/internal/chat"

	"github.com/sixath/framework/tool"
)

// buildRunCtxLikeChat mirrors chat.SendMessage runCtx after WireCatalogAndToolSearch.
func buildRunCtxLikeChat(parent context.Context, catalog tool.ToolCatalog, toolSearchActive bool) context.Context {
	runCtx := context.WithValue(parent, tool.ContextKeyToolCatalog, catalog)
	if toolSearchActive {
		runCtx = context.WithValue(runCtx, tool.ContextKeyToolSearchActive, true)
	}
	return runCtx
}

func registerServiceMcpTools(t *testing.T, reg *tool.Registry, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("mcp__svc__tool_%03d", i)
		desc := fmt.Sprintf("Service-level MCP deferred tool for SATH_TOOL_SEARCH regression testing entry #%d", i)
		if err := reg.Register(tool.Tool{
			Name:        name,
			Description: desc,
			Toolset:     tool.ToolsetMCP,
			Parameters:  map[string]any{"type": "object"},
			Execute:     func(context.Context, map[string]any) (any, error) { return nil, nil },
		}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
}

func TestChatWiring_ToolSearchOff_FullInlineSchema(t *testing.T) {
	t.Setenv("SATH_TOOL_SEARCH", "off")
	defer os.Unsetenv("SATH_TOOL_SEARCH")

	reg := tool.NewRegistry()
	registerServiceMcpTools(t, reg, 12)
	if err := chat.RegisterAskUserTools(reg); err != nil {
		t.Fatal(err)
	}

	catalog, active, err := chat.WireCatalogAndToolSearch(context.Background(), chat.CatalogWiringInput{Reg: reg})
	if err != nil {
		t.Fatalf("WireCatalogAndToolSearch: %v", err)
	}
	if active {
		t.Fatal("SATH_TOOL_SEARCH=off should not activate tool_search bridge")
	}
	if _, ok := reg.Get(tool.ToolSearchName); ok {
		t.Fatal("tool_search should not be registered when off")
	}

	runCtx := buildRunCtxLikeChat(context.Background(), catalog, active)
	inline := reg.ListForAPI(runCtx, nil)
	exposed := reg.ListForAPIWithDefer(runCtx, nil, active)
	if len(exposed) != len(inline) {
		t.Fatalf("off mode: ListForAPIWithDefer should match ListForAPI (inline=%d exposed=%d)", len(inline), len(exposed))
	}
	mcpCount := 0
	for _, tl := range exposed {
		if strings.HasPrefix(tl.Name, "mcp__svc__") {
			mcpCount++
		}
	}
	if mcpCount != 12 {
		t.Fatalf("off mode: all MCP tools should appear inline, got %d", mcpCount)
	}
}

func TestChatWiring_ToolSearchOn_ReducesDeferredSchema(t *testing.T) {
	t.Setenv("SATH_TOOL_SEARCH", "on")
	defer os.Unsetenv("SATH_TOOL_SEARCH")

	reg := tool.NewRegistry()
	registerServiceMcpTools(t, reg, 8)

	catalog, active, err := chat.WireCatalogAndToolSearch(context.Background(), chat.CatalogWiringInput{Reg: reg})
	if err != nil {
		t.Fatalf("WireCatalogAndToolSearch: %v", err)
	}
	if !active {
		t.Fatal("SATH_TOOL_SEARCH=on with deferred MCP should activate bridge")
	}

	runCtx := buildRunCtxLikeChat(context.Background(), catalog, active)
	inline := reg.ListForAPI(runCtx, nil)
	exposed := reg.ListForAPIWithDefer(runCtx, nil, active)
	if len(exposed) >= len(inline) {
		t.Fatalf("on mode: deferred schema should be smaller (inline=%d exposed=%d)", len(inline), len(exposed))
	}
	for _, tl := range exposed {
		if strings.HasPrefix(tl.Name, "mcp__svc__") {
			t.Fatalf("deferred MCP %q should be excluded when tool_search active", tl.Name)
		}
	}
	if _, ok := reg.Get(tool.ToolSearchName); !ok {
		t.Fatal("tool_search bridge should be registered when on")
	}
}
