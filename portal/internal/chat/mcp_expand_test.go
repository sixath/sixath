package chat

import (
	"context"
	"strings"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
)

func TestMatchBoundMCPServers_EmptyQueryAllUnbound(t *testing.T) {
	reg := tool.NewRegistry()
	servers := []*biz.McpServerMeta{
		{ID: "gitlab", Name: "gitlab"},
		{ID: "confluence", Name: "Confluence DC"},
	}
	got := matchBoundMCPServers("", servers, reg)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
}

func TestMatchBoundMCPServers_SkipsAlreadyRegistered(t *testing.T) {
	reg := tool.NewRegistry()
	reg.MarkMcpServer("gitlab")
	servers := []*biz.McpServerMeta{
		{ID: "gitlab", Name: "gitlab"},
		{ID: "confluence", Name: "Confluence DC"},
	}
	got := matchBoundMCPServers("", servers, reg)
	if len(got) != 1 || got[0].ID != "confluence" {
		t.Fatalf("got %#v", got)
	}
}

func TestMatchBoundMCPServers_QueryPrefersMetaHit(t *testing.T) {
	reg := tool.NewRegistry()
	servers := []*biz.McpServerMeta{
		{ID: "gitlab", Name: "gitlab"},
		{ID: "confluence", Name: "Confluence DC", Description: "Atlassian wiki"},
	}
	got := matchBoundMCPServers("search confluence docs", servers, reg)
	if len(got) != 1 || got[0].ID != "confluence" {
		t.Fatalf("got %#v", got)
	}
}

func TestMatchBoundMCPServers_QueryMissDoesNotLoadUnrelated(t *testing.T) {
	reg := tool.NewRegistry()
	servers := []*biz.McpServerMeta{
		{ID: "gitlab", Name: "gitlab"},
		{ID: "confluence", Name: "Confluence DC"},
	}
	got := matchBoundMCPServers("lightstreamer privileged", servers, reg)
	if len(got) != 0 {
		t.Fatalf("unrelated query must not expand MCP, got %#v", got)
	}
}

func TestMcpExpandOnMiss_ListToolsLoadsUnbound(t *testing.T) {
	t.Setenv("SATH_MCP_EXPAND_ON_MISS", "")
	reg := tool.NewRegistry()
	_ = tool.RegisterListToolsTool(reg, nil)

	// Pretend confluence tools already known via a stub Register after expand —
	// use a fake by marking after Expand would call RegisterMcpTool.
	// Here we unit-test controller against MarkMcpServer path by injecting a
	// server that RegisterMcpTool cannot start; instead test Expand updates
	// active families when Register succeeds via HTTP MCP stub... keep it light:
	// only verify match + NewMcpExpandOnMiss wiring + list_tools expand hook with fake expander.

	fake := &fakeExpand{
		cat: tool.ToolCatalog{Entries: []tool.ToolCatalogEntry{
			{Name: "list_tools", Toolset: tool.ToolsetCore, Available: true},
		}},
	}
	ctx := context.WithValue(context.Background(), tool.ContextKeyToolDiscoveryExpand, fake)
	ctx = context.WithValue(ctx, tool.ContextKeyToolCatalog, fake.cat)
	lt, ok := reg.Get("list_tools")
	if !ok {
		t.Fatal("list_tools missing")
	}
	out, err := lt.Execute(ctx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !fake.expandedEmpty {
		t.Fatal("expected ExpandOnMiss(\"\") on list_tools without query")
	}
	s, _ := out.(string)
	if !strings.Contains(s, "confluence_searchContent") {
		t.Fatalf("expected expanded catalog tools in output, got %s", s)
	}
}

type fakeExpand struct {
	cat           tool.ToolCatalog
	expandedEmpty bool
}

func (f *fakeExpand) CurrentCatalog() tool.ToolCatalog { return f.cat }

func (f *fakeExpand) ExpandOnMiss(_ context.Context, query string) ([]string, error) {
	if query == "" {
		f.expandedEmpty = true
		f.cat = tool.ToolCatalog{Entries: []tool.ToolCatalogEntry{
			{Name: "list_tools", Toolset: tool.ToolsetCore, Available: true},
			{Name: "confluence_searchContent", Toolset: tool.ToolsetMCP, Available: true, Deferred: true,
				Bindings: map[string]string{"mcp_server": "confluence"}},
		}}
		return []string{"confluence"}, nil
	}
	return nil, nil
}

func TestMcpExpandOnMiss_DisabledByEnv(t *testing.T) {
	t.Setenv("SATH_MCP_EXPAND_ON_MISS", "0")
	exp := NewMcpExpandOnMiss(McpExpandOnMissOptions{
		Reg:          tool.NewRegistry(),
		BoundServers: []*biz.McpServerMeta{{ID: "confluence"}},
	})
	if exp != nil {
		t.Fatal("expected nil when disabled")
	}
}

func TestMcpMetaMatchesQuery(t *testing.T) {
	s := &biz.McpServerMeta{ID: "confluence", Name: "Confluence DC", Description: "wiki"}
	if !mcpMetaMatchesQuery("confluence文档", s) {
		t.Fatal("expected match")
	}
	if mcpMetaMatchesQuery("jira only", s) {
		t.Fatal("expected no match")
	}
}
