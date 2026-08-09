package tool

import (
	"context"
	"strings"
)

// McpCatalogProvider 为 MCP 工具补充 server 拆词 SearchHints。
type McpCatalogProvider struct{}

func (p *McpCatalogProvider) Enrich(_ context.Context, entries []ToolCatalogEntry) []ToolCatalogEntry {
	out := make([]ToolCatalogEntry, len(entries))
	for i, e := range entries {
		out[i] = e
		server := ""
		if e.Bindings != nil {
			server = e.Bindings["mcp_server"]
		}
		if server == "" {
			continue
		}
		out[i].SearchHints = mergeSearchHints(splitMcpServerHints(server, e.Name), e.SearchHints)
	}
	return out
}

// splitMcpServerHints 将 MCP server ID 与工具名拆为 BM25 检索词。
func splitMcpServerHints(serverID, toolName string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	splitParts := func(s string) {
		add(s)
		for _, part := range strings.FieldsFunc(s, func(r rune) bool {
			return r == '_' || r == '-' || r == '/' || r == ':' || r == '.'
		}) {
			add(part)
		}
	}
	splitParts(serverID)
	splitParts(toolName)
	return out
}
