package tool

import (
	"context"
	"strings"
	"testing"
)

func TestListTools_ExecuteAll(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(Tool{
		Name: "web_search", Toolset: ToolsetWeb, Description: "search",
		Execute: func(ctx context.Context, params map[string]any) (any, error) { return nil, nil },
	})
	cat := BuildToolCatalog(context.Background(), reg, &BuiltinHintProvider{})
	if err := RegisterListToolsTool(reg, nil); err != nil {
		t.Fatalf("RegisterListToolsTool: %v", err)
	}
	ctx := context.WithValue(context.Background(), ContextKeyToolCatalog, cat)
	tool, ok := reg.Get("list_tools")
	if !ok {
		t.Fatal("list_tools not registered")
	}
	out, err := tool.Execute(ctx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := out.(string)
	if !ok || !strings.Contains(s, "web_search") {
		t.Fatalf("expected catalog json with web_search, got %v", out)
	}
}

func TestListTools_QueryFilter(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(Tool{
		Name: "web_search", Toolset: ToolsetWeb, Description: "search web",
		Execute: func(ctx context.Context, params map[string]any) (any, error) { return nil, nil },
	})
	_ = reg.Register(Tool{
		Name: "send_to_wecom", Toolset: ToolsetCore,
		Description: "Push message to WeCom group",
		SearchHints: []string{"企微", "企业微信"},
		Execute: func(ctx context.Context, params map[string]any) (any, error) { return nil, nil },
	})
	cat := BuildToolCatalog(context.Background(), reg, &BuiltinHintProvider{})
	if err := RegisterListToolsTool(reg, nil); err != nil {
		t.Fatalf("RegisterListToolsTool: %v", err)
	}
	ctx := context.WithValue(context.Background(), ContextKeyToolCatalog, cat)
	tool, ok := reg.Get("list_tools")
	if !ok {
		t.Fatal("list_tools not registered")
	}
	out, err := tool.Execute(ctx, map[string]any{"query": "企微"})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := out.(string)
	if !ok || !strings.Contains(s, "send_to_wecom") {
		t.Fatalf("expected send_to_wecom in results, got %v", out)
	}
	if strings.Contains(s, "web_search") {
		t.Fatalf("web_search should not match query 企微, got %s", s)
	}
}

func TestListTools_NoCatalog(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterListToolsTool(reg, nil); err != nil {
		t.Fatalf("RegisterListToolsTool: %v", err)
	}
	tool, ok := reg.Get("list_tools")
	if !ok {
		t.Fatal("list_tools not registered")
	}
	out, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := out.(string)
	if !ok || s != "list_tools: no catalog in context" {
		t.Fatalf("expected no catalog message, got %v", out)
	}
}
