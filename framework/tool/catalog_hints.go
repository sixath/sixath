package tool

import "context"

var defaultToolsetHints = map[string][]string{
	ToolsetFile:          {"文件", "读写", "SQL", "查询", "表", "数据库"},
	ToolsetWeb:           {"搜索", "网页", "抓取", "联网"},
	ToolsetSkills:        {"技能", "skill", "脚本"},
	ToolsetMemory:        {"记忆", "历史", "会话"},
	ToolsetSessionSearch: {"跨会话", "搜索", "历史对话"},
	ToolsetTerminal:      {"SSH", "终端", "远程", "命令"},
	ToolsetCronjob:       {"定时", "计划任务", "cron"},
	ToolsetTodo:          {"待办", "任务列表"},
	ToolsetCore:          {"用户输入", "确认", "工具目录"},
}

// BuiltinHintProvider 按 toolset 补充默认 SearchHints。
type BuiltinHintProvider struct{}

func (p *BuiltinHintProvider) Enrich(_ context.Context, entries []ToolCatalogEntry) []ToolCatalogEntry {
	out := make([]ToolCatalogEntry, len(entries))
	for i, e := range entries {
		out[i] = e
		defaults := defaultToolsetHints[e.Toolset]
		if len(defaults) == 0 && len(e.SearchHints) == 0 {
			continue
		}
		out[i].SearchHints = mergeSearchHints(defaults, e.SearchHints)
	}
	return out
}

func mergeSearchHints(defaults, existing []string) []string {
	seen := make(map[string]struct{}, len(defaults)+len(existing))
	out := make([]string, 0, len(defaults)+len(existing))
	for _, h := range existing {
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	for _, h := range defaults {
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}
