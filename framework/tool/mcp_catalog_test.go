package tool

import (
	"context"
	"slices"
	"testing"
)

func TestMcpCatalogProvider_EnrichesServerHints(t *testing.T) {
	p := &McpCatalogProvider{}
	entries := []ToolCatalogEntry{{
		Name:    "create_issue",
		Toolset: ToolsetMCP,
		Bindings: map[string]string{
			"mcp_server": "plugin-jira-jira",
		},
	}}
	out := p.Enrich(context.Background(), entries)
	if len(out) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(out))
	}
	for _, want := range []string{"plugin-jira-jira", "plugin", "jira", "create_issue", "create", "issue"} {
		if !slices.Contains(out[0].SearchHints, want) {
			t.Fatalf("SearchHints missing %q: %v", want, out[0].SearchHints)
		}
	}
}

func TestSplitMcpServerHints(t *testing.T) {
	hints := splitMcpServerHints("user-jira", "create_issue")
	if !slices.Contains(hints, "user-jira") || !slices.Contains(hints, "jira") || !slices.Contains(hints, "create_issue") {
		t.Fatalf("unexpected hints: %v", hints)
	}
}
