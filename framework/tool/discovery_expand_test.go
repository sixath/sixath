package tool

import (
	"context"
	"strings"
	"testing"
)

type stubExpand struct {
	cat       ToolCatalog
	calls     []string
	onExpand  func(query string) []string
}

func (s *stubExpand) CurrentCatalog() ToolCatalog { return s.cat }

func (s *stubExpand) ExpandOnMiss(_ context.Context, query string) ([]string, error) {
	s.calls = append(s.calls, query)
	if s.onExpand != nil {
		ids := s.onExpand(query)
		return ids, nil
	}
	return nil, nil
}

func TestListTools_ExpandOnMissQueryRetry(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterListToolsTool(reg, nil); err != nil {
		t.Fatal(err)
	}
	exp := &stubExpand{
		cat: ToolCatalog{Entries: []ToolCatalogEntry{
			{Name: "list_tools", Toolset: ToolsetCore, Available: true},
		}},
	}
	exp.onExpand = func(query string) []string {
		if query != "confluence" {
			t.Fatalf("query=%q", query)
		}
		exp.cat = ToolCatalog{Entries: []ToolCatalogEntry{
			{Name: "list_tools", Toolset: ToolsetCore, Available: true},
			{Name: "confluence_searchContent", Toolset: ToolsetMCP, Available: true,
				Description: "search confluence", SearchHints: []string{"confluence"}},
		}}
		return []string{"confluence"}
	}
	ctx := context.WithValue(context.Background(), ContextKeyToolDiscoveryExpand, exp)
	ctx = context.WithValue(ctx, ContextKeyToolCatalog, exp.cat)
	lt, _ := reg.Get("list_tools")
	out, err := lt.Execute(ctx, map[string]any{"query": "confluence"})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := out.(string)
	if !strings.Contains(s, "confluence_searchContent") {
		t.Fatalf("got %s", s)
	}
	if len(exp.calls) != 1 || exp.calls[0] != "confluence" {
		t.Fatalf("calls=%v", exp.calls)
	}
}

func TestToolSearch_ExpandOnMiss(t *testing.T) {
	reg := NewRegistry()
	exp := &stubExpand{
		cat: ToolCatalog{Entries: []ToolCatalogEntry{
			{Name: "tool_search", Toolset: ToolsetCore, Available: true},
		}},
	}
	exp.onExpand = func(query string) []string {
		exp.cat = ToolCatalog{Entries: []ToolCatalogEntry{
			{Name: "tool_search", Toolset: ToolsetCore, Available: true},
			{Name: "confluence_getContent", Toolset: ToolsetMCP, Available: true, Deferred: true,
				Description: "get confluence page", SearchHints: []string{"confluence"}},
		}}
		return []string{"confluence"}
	}
	if err := RegisterToolSearchTools(reg, ToolSearchRegisterConfig{Registry: reg, Catalog: exp.cat}); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), ContextKeyToolDiscoveryExpand, exp)
	ts, _ := reg.Get(ToolSearchName)
	out, err := ts.Execute(ctx, map[string]any{"query": "confluence"})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := out.(string)
	if !strings.Contains(s, "confluence_getContent") {
		t.Fatalf("got %s", s)
	}
}
