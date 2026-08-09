package tool

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestShouldActivateToolSearch_Off(t *testing.T) {
	cat := ToolCatalog{Entries: []ToolCatalogEntry{{
		Name: "web_search", Toolset: ToolsetWeb, Available: true, Deferred: true,
	}}}
	cfg := ToolSearchConfig{Mode: "off"}
	if ShouldActivateToolSearch(cat, cfg) {
		t.Fatal("off mode should not activate")
	}
}

func TestShouldActivateToolSearch_OnWithDeferred(t *testing.T) {
	cat := ToolCatalog{Entries: []ToolCatalogEntry{{
		Name: "web_search", Toolset: ToolsetWeb, Available: true, Deferred: true,
	}}}
	cfg := ToolSearchConfig{Mode: "on"}
	if !ShouldActivateToolSearch(cat, cfg) {
		t.Fatal("on mode with deferred available should activate")
	}
}

func TestShouldActivateToolSearch_AutoMcpBulk(t *testing.T) {
	reg := NewRegistry()
	registerBulkMcpTools(t, reg, 75)
	cat := BuildToolCatalog(context.Background(), reg)
	cfg := ToolSearchConfig{
		Mode:               "auto",
		ThresholdPct:       10,
		HardTokenThreshold: 20000,
	}
	if !ShouldActivateToolSearch(cat, cfg) {
		t.Fatalf("auto mode with 75 deferred MCP tools should exceed 10%% token threshold (estimated=%d)",
			estimateDeferredSchemaTokens(cat))
	}
	if len(cat.Entries) < 50 {
		t.Fatalf("expected at least 50 deferred MCP entries, got %d", len(cat.Entries))
	}
}

func registerBulkMcpTools(t *testing.T, reg *Registry, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("mcp__bulk__tool_%03d", i)
		desc := fmt.Sprintf("Deferred MCP integration tool for progressive disclosure auto-activation threshold testing entry #%d", i)
		if err := reg.Register(Tool{
			Name:        name,
			Description: desc,
			Toolset:     ToolsetMCP,
			Parameters:  map[string]any{"type": "object"},
			Execute:     func(context.Context, map[string]any) (any, error) { return nil, nil },
		}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
}

func TestShouldActivateToolSearch_AutoBelowThreshold(t *testing.T) {
	cat := ToolCatalog{Entries: []ToolCatalogEntry{{
		Name: "web_search", Toolset: ToolsetWeb, Available: true, Deferred: true,
		Description: "search",
	}}}
	cfg := ToolSearchConfig{
		Mode:               "auto",
		ThresholdPct:       10,
		HardTokenThreshold: 20000,
	}
	if ShouldActivateToolSearch(cat, cfg) {
		t.Fatal("auto mode below token threshold should not activate")
	}
}

func TestToolSearch_ExecuteReturnsNames(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(Tool{
		Name: "mcp__jira__create_issue", Toolset: ToolsetMCP,
		Description: "Create a Jira issue",
		SearchHints: []string{"jira", "issue"},
		Execute:     func(ctx context.Context, params map[string]any) (any, error) { return nil, nil },
	})
	_ = reg.Register(Tool{
		Name: "web_search", Toolset: ToolsetWeb, Description: "search web",
		Execute: func(ctx context.Context, params map[string]any) (any, error) { return nil, nil },
	})
	cat := BuildToolCatalog(context.Background(), reg, &BuiltinHintProvider{})
	if err := RegisterToolSearchTools(reg, ToolSearchRegisterConfig{Registry: reg, Catalog: cat}); err != nil {
		t.Fatalf("RegisterToolSearchTools: %v", err)
	}
	tool, ok := reg.Get(ToolSearchName)
	if !ok {
		t.Fatal("tool_search not registered")
	}
	out, err := tool.Execute(context.Background(), map[string]any{"query": "jira issue"})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := out.(string)
	if !ok || !strings.Contains(s, "mcp__jira__create_issue") {
		t.Fatalf("expected jira tool in results, got %v", out)
	}
	if strings.Contains(s, "web_search") {
		t.Fatalf("web_search is not deferred, should not appear in tool_search results: %s", s)
	}
}

func TestToolDescribe_ReturnsSchema(t *testing.T) {
	reg := NewRegistry()
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"q": map[string]any{"type": "string"},
		},
	}
	_ = reg.Register(Tool{
		Name: "echo_tool", Toolset: ToolsetWeb, Description: "echo back",
		Parameters: params,
		Execute:    func(ctx context.Context, p map[string]any) (any, error) { return nil, nil },
	})
	cat := BuildToolCatalog(context.Background(), reg)
	if err := RegisterToolSearchTools(reg, ToolSearchRegisterConfig{Registry: reg, Catalog: cat}); err != nil {
		t.Fatalf("RegisterToolSearchTools: %v", err)
	}
	tool, ok := reg.Get(ToolDescribeName)
	if !ok {
		t.Fatal("tool_describe not registered")
	}
	out, err := tool.Execute(context.Background(), map[string]any{"name": "echo_tool"})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("expected string json, got %T", out)
	}
	if !strings.Contains(s, "echo_tool") || !strings.Contains(s, "echo back") || !strings.Contains(s, `"q"`) {
		t.Fatalf("expected full schema in describe output, got %s", s)
	}
}

func TestToolCall_UnwrapsToRealTool(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(Tool{
		Name: "echo", Toolset: ToolsetCore, Description: "echo message",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{"type": "string"},
			},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			msg, _ := params["message"].(string)
			return "echo:" + msg, nil
		},
	})
	cat := BuildToolCatalog(context.Background(), reg)
	if err := RegisterToolSearchTools(reg, ToolSearchRegisterConfig{Registry: reg, Catalog: cat}); err != nil {
		t.Fatalf("RegisterToolSearchTools: %v", err)
	}
	tool, ok := reg.Get(ToolCallName)
	if !ok {
		t.Fatal("tool_call not registered")
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"name":      "echo",
		"arguments": map[string]any{"message": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "echo:hello" {
		t.Fatalf("expected echo:hello, got %v", out)
	}
}
