package tool

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

// ListToolsConfig 构造 list_tools 工具的可选配置（当前无字段，预留扩展）。
type ListToolsConfig struct{}

// listToolsEntry 为 list_tools 输出的单条工具摘要。
type listToolsEntry struct {
	Name        string            `json:"name"`
	Toolset     string            `json:"toolset"`
	Description string            `json:"description,omitempty"`
	Bindings    map[string]string `json:"bindings,omitempty"`
	SearchHints []string          `json:"search_hints,omitempty"`
}

// listToolsGroupedResponse 无 query 时按 toolset 分组返回。
type listToolsGroupedResponse struct {
	Grouped map[string][]listToolsEntry `json:"grouped"`
	Count   int                         `json:"count"`
}

// listToolsFlatResponse 有 query 时返回扁平结果列表。
type listToolsFlatResponse struct {
	Tools []listToolsEntry `json:"tools"`
	Count int              `json:"count"`
}

// RegisterListToolsTool 向注册表注册 list_tools 工具。
func RegisterListToolsTool(reg *Registry, cfg *ListToolsConfig) error {
	_ = cfg
	return reg.Register(Tool{
		Name:        "list_tools",
		Description: "List or search tools available to this agent. Use before guessing tool names.",
		Toolset:     ToolsetCore,
		AlwaysLoad:  true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Optional BM25 search query to filter tools by name, description, or hints.",
				},
				"toolset": map[string]any{
					"type":        "string",
					"description": "Optional toolset filter (e.g. web, file, core).",
				},
				"available_only": map[string]any{
					"type":        "boolean",
					"description": "When true (default), only return tools that are currently available.",
				},
			},
		},
		Execute: buildListToolsExecute(cfg),
	})
}

func buildListToolsExecute(cfg *ListToolsConfig) ExecuteFunc {
	_ = cfg
	return func(ctx context.Context, params map[string]any) (any, error) {
		cat, ok := CatalogFromContext(ctx)
		if !ok {
			return "list_tools: no catalog in context", nil
		}

		availableOnly := true
		if v, ok := params["available_only"].(bool); ok {
			availableOnly = v
		}

		toolsetFilter, _ := params["toolset"].(string)
		toolsetFilter = strings.TrimSpace(toolsetFilter)

		query, _ := params["query"].(string)
		query = strings.TrimSpace(query)

		// 无 query：先尝试装载尚未进入目录的绑定 MCP，再返回完整分组列表。
		if query == "" {
			if exp := DiscoveryExpandFromContext(ctx); exp != nil {
				if _, err := exp.ExpandOnMiss(ctx, ""); err == nil {
					cat = exp.CurrentCatalog()
				}
			}
			entries := filterCatalogEntries(cat.Entries, availableOnly, toolsetFilter)
			return marshalListToolsGrouped(entries)
		}

		entries := filterCatalogEntries(cat.Entries, availableOnly, toolsetFilter)
		filtered := ToolCatalog{Entries: entries, GeneratedAt: cat.GeneratedAt}
		results := SearchCatalog(filtered, query, 20)
		if len(results) == 0 {
			if exp := DiscoveryExpandFromContext(ctx); exp != nil {
				if expanded, err := exp.ExpandOnMiss(ctx, query); err == nil && len(expanded) > 0 {
					cat = exp.CurrentCatalog()
					entries = filterCatalogEntries(cat.Entries, availableOnly, toolsetFilter)
					filtered = ToolCatalog{Entries: entries, GeneratedAt: cat.GeneratedAt}
					results = SearchCatalog(filtered, query, 20)
				}
			}
		}
		return marshalListToolsFlat(results)
	}
}

func filterCatalogEntries(entries []ToolCatalogEntry, availableOnly bool, toolset string) []ToolCatalogEntry {
	out := make([]ToolCatalogEntry, 0, len(entries))
	for _, e := range entries {
		if availableOnly && !e.Available {
			continue
		}
		if toolset != "" && e.Toolset != toolset {
			continue
		}
		out = append(out, e)
	}
	return out
}

func toListToolsEntry(e ToolCatalogEntry) listToolsEntry {
	entry := listToolsEntry{
		Name:        e.Name,
		Toolset:     e.Toolset,
		Description: e.Description,
	}
	if len(e.Bindings) > 0 {
		entry.Bindings = e.Bindings
	}
	if len(e.SearchHints) > 0 {
		entry.SearchHints = e.SearchHints
	}
	return entry
}

func marshalListToolsGrouped(entries []ToolCatalogEntry) (string, error) {
	grouped := make(map[string][]listToolsEntry)
	toolsets := make([]string, 0)
	for _, e := range entries {
		ts := e.Toolset
		if ts == "" {
			ts = "_ungrouped"
		}
		if _, ok := grouped[ts]; !ok {
			toolsets = append(toolsets, ts)
		}
		grouped[ts] = append(grouped[ts], toListToolsEntry(e))
	}
	sort.Strings(toolsets)
	for _, ts := range toolsets {
		sort.Slice(grouped[ts], func(i, j int) bool {
			return grouped[ts][i].Name < grouped[ts][j].Name
		})
	}
	resp := listToolsGroupedResponse{Grouped: grouped, Count: len(entries)}
	b, err := json.Marshal(resp)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func marshalListToolsFlat(entries []ToolCatalogEntry) (string, error) {
	tools := make([]listToolsEntry, 0, len(entries))
	for _, e := range entries {
		tools = append(tools, toListToolsEntry(e))
	}
	resp := listToolsFlatResponse{Tools: tools, Count: len(tools)}
	b, err := json.Marshal(resp)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
