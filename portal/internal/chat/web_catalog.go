package chat

import (
	"context"

	"github.com/sixath/framework/tool"
)

var webCatalogToolNames = map[string]struct{}{
	"web_search":  {},
	"web_extract": {},
}

// WebToolsCatalogProvider 将当前 web 搜索 backend 注入 web 工具的 Bindings 与 SearchHints。
type WebToolsCatalogProvider struct{}

func (p *WebToolsCatalogProvider) Enrich(_ context.Context, entries []tool.ToolCatalogEntry) []tool.ToolCatalogEntry {
	backend := effectiveSearchBackend(WebSettingsSnapshot())
	if backend == "" {
		return entries
	}

	bindings := map[string]string{"search_backend": backend}
	hints := []string{backend, "搜索", "联网"}

	out := make([]tool.ToolCatalogEntry, len(entries))
	for i, e := range entries {
		out[i] = e
		if _, ok := webCatalogToolNames[e.Name]; !ok {
			continue
		}
		out[i].Bindings = bindings
		out[i].SearchHints = mergeCatalogHints(e.SearchHints, hints...)
	}
	return out
}
