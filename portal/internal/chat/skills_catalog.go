package chat

import (
	"context"

	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
)

var skillsCatalogToolNames = map[string]struct{}{
	"load_skill":           {},
	"skills_list":          {},
	"read_skill_file":      {},
	"execute_skill_script": {},
}

// SkillsCatalogProvider 将已安装 skill 名/描述注入技能相关工具的 SearchHints。
type SkillsCatalogProvider struct {
	Index *skills.Index
}

func (p *SkillsCatalogProvider) Enrich(_ context.Context, entries []tool.ToolCatalogEntry) []tool.ToolCatalogEntry {
	if p.Index == nil {
		return entries
	}
	metas := p.Index.All()
	if len(metas) == 0 {
		return entries
	}

	var hintParts []string
	for _, m := range metas {
		if m.Name != "" {
			hintParts = append(hintParts, m.Name)
		}
		if m.Description != "" {
			hintParts = append(hintParts, m.Description)
		}
	}

	out := make([]tool.ToolCatalogEntry, len(entries))
	for i, e := range entries {
		out[i] = e
		if _, ok := skillsCatalogToolNames[e.Name]; !ok {
			continue
		}
		out[i].SearchHints = mergeCatalogHints(e.SearchHints, hintParts...)
	}
	return out
}
