package tool

import (
	"context"
	"time"
)

// ToolCatalogEntry 为单条工具目录项（enrich 后供 list_tools / prompt / BM25 使用）。
type ToolCatalogEntry struct {
	Name, Toolset, Description string
	SearchHints                []string
	Bindings                   map[string]string
	Available, Deferred        bool
	RelatedTools               []string
}

// ToolCatalog 为当前 Registry 快照。
type ToolCatalog struct {
	Entries     []ToolCatalogEntry
	GeneratedAt time.Time
}

// CatalogProvider 对目录条目做 enrich（SearchHints、Bindings 等）。
type CatalogProvider interface {
	Enrich(ctx context.Context, entries []ToolCatalogEntry) []ToolCatalogEntry
}

// BuildToolCatalog 从 Registry 构建工具目录；依次调用 providers，末尾补 BuiltinHintProvider。
func BuildToolCatalog(ctx context.Context, reg *Registry, providers ...CatalogProvider) ToolCatalog {
	tools := reg.List()
	entries := make([]ToolCatalogEntry, 0, len(tools))
	for _, t := range tools {
		available := true
		if t.CheckFn != nil {
			if err := t.CheckFn(ctx); err != nil {
				continue
			}
		}
		entry := ToolCatalogEntry{
			Name:        t.Name,
			Toolset:     t.Toolset,
			Description: t.Description,
			Available:   available,
			Deferred:    ShouldDefer(t, DefaultDeferConfig()),
		}
		if len(t.SearchHints) > 0 {
			entry.SearchHints = append([]string(nil), t.SearchHints...)
		}
		if len(t.Bindings) > 0 {
			entry.Bindings = make(map[string]string, len(t.Bindings))
			for k, v := range t.Bindings {
				entry.Bindings[k] = v
			}
		}
		entries = append(entries, entry)
	}

	hasBuiltin := false
	for _, p := range providers {
		if _, ok := p.(*BuiltinHintProvider); ok {
			hasBuiltin = true
		}
		entries = p.Enrich(ctx, entries)
	}
	if !hasBuiltin {
		entries = (&BuiltinHintProvider{}).Enrich(ctx, entries)
	}

	return ToolCatalog{
		Entries:     entries,
		GeneratedAt: time.Now(),
	}
}
