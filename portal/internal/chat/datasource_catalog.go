package chat

import (
	"context"

	"github.com/sixath/framework/tool"
)

var datasourceCatalogToolNames = map[string]struct{}{
	"list_tables":    {},
	"describe_table": {},
	"execute_read":   {},
}

// DatasourceCatalogProvider 为数据源相关工具注入绑定元数据与检索词。
type DatasourceCatalogProvider struct {
	Bindings []DatasourceBinding
}

func (p *DatasourceCatalogProvider) Enrich(_ context.Context, entries []tool.ToolCatalogEntry) []tool.ToolCatalogEntry {
	available := make([]DatasourceBinding, 0, len(p.Bindings))
	for _, b := range p.Bindings {
		if b.Available {
			available = append(available, b)
		}
	}
	if len(available) == 0 {
		return entries
	}

	var hintParts []string
	for _, b := range available {
		if b.Type != "" {
			hintParts = append(hintParts, b.Type)
		}
		if b.DBName != "" {
			hintParts = append(hintParts, b.DBName)
		}
		hintParts = append(hintParts, "mysql", "数据库", "SQL")
		if b.ID != "" {
			hintParts = append(hintParts, b.ID)
		}
	}

	out := make([]tool.ToolCatalogEntry, len(entries))
	for i, e := range entries {
		out[i] = e
		if _, ok := datasourceCatalogToolNames[e.Name]; !ok {
			continue
		}
		out[i].SearchHints = mergeCatalogHints(e.SearchHints, hintParts...)
		if len(available) == 1 {
			b := available[0]
			out[i].Bindings = map[string]string{
				"datasource_id": b.ID,
				"type":          b.Type,
				"db_name":       b.DBName,
			}
		}
	}
	return out
}
