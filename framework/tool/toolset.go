package tool

// 与 Hermes Agent（toolsets.py）对齐的工具集标签：用于按场景过滤 schema、配置白名单及文档化。
// 参考: https://github.com/NousResearch/hermes-agent/blob/main/toolsets.py
//
// Sixath 说明：
//   - web / skills / memory / terminal 与 Hermes 语义接近。
//   - Hermes 的 file 为 read_file、write_file、patch、search_files；本仓库将「数据源只读/只写与元数据」
//     暂归入同一标签 ToolsetFile，便于预设「开发+数据」一体；若后续增加纯 workspace 文件工具，仍用 file。
//   - 动态 MCP 工具统一为 ToolsetMCP（Hermes 侧为动态 toolset）。

const (
	// ToolsetWeb Web 请求与可扩展的抓取类能力（Hermes: web_search, web_extract）。
	ToolsetWeb = "web"
	// ToolsetFile 工作区/数据源读写与结构探查（Hermes: read_file, write_file, patch, search_files；Sixath 含 execute_*、表元数据）。
	ToolsetFile = "file"
	// ToolsetSkills 技能加载与脚本执行（Hermes: skills_list, skill_view, skill_manage）。
	ToolsetSkills = "skills"
	// ToolsetMemory MemoryStore facade 工具（Hermes: memory）。
	ToolsetMemory = "memory"
	// ToolsetSessionSearch remains available for custom catalog entries. The
	// legacy session_search tool itself is no longer registered.
	ToolsetSessionSearch = "session_search"
	// ToolsetTerminal 远程 shell / 进程类（Hermes: terminal, process）。
	ToolsetTerminal = "terminal"
	// ToolsetMCP 由 MCP 服务动态注册的工具（Hermes 为运行时注入的 MCP toolset）。
	ToolsetMCP = "mcp"
	// ToolsetCore 人机协同核心工具（ask_user 等）。
	ToolsetCore = "core"
	// ToolsetTodo 会话内任务表（Hermes: todo）。
	ToolsetTodo = "todo"
	// ToolsetCronjob 定时任务管理（Hermes: cronjob）。
	ToolsetCronjob = "cronjob"
	// ToolsetBrowser 浏览器自动化（Hermes: browser_*）。
	ToolsetBrowser = "browser"
)

// PresetHermesCoreTags 与 Hermes _HERMES_CORE_TOOLS 中「web + file + skills + memory + terminal」五类标签一致，
// 不含 browser/mcp 等；用于按标签快速启用一组工具集。
func PresetHermesCoreTags() []string {
	return []string{ToolsetWeb, ToolsetFile, ToolsetSkills, ToolsetMemory, ToolsetTerminal}
}

// builtinDefaultToolset 为内置工具名到默认 toolset 的映射；Register 时若 Tool.Toolset 为空则自动填入。
var builtinDefaultToolset = map[string]string{
	"http_request": ToolsetWeb,

	"execute_read":         ToolsetFile,
	"execute_write":        ToolsetFile,
	"read_file":            ToolsetFile,
	"write_file":           ToolsetFile,
	"patch":                ToolsetFile,
	"search_files":         ToolsetFile,
	"web_search":           ToolsetWeb,
	"web_extract":          ToolsetWeb,
	"ask_user":             ToolsetCore,
	"list_tables":          ToolsetFile,
	"describe_table":       ToolsetFile,
	"load_skill":           ToolsetSkills,
	"read_skill_file":      ToolsetSkills,
	"execute_skill_script": ToolsetSkills,
	"skills_list":          ToolsetSkills,
	"skill_view":           ToolsetSkills,
	"skill_manage":         ToolsetSkills,

	"memory_remember": ToolsetMemory,
	"memory_recall":   ToolsetMemory,
	"memory_get":      ToolsetMemory,
	"todo":            ToolsetTodo,

	"ssh_exec": ToolsetTerminal,
	"scp":      ToolsetTerminal,
	"terminal": ToolsetTerminal,
	"process":  ToolsetTerminal,
	"cronjob":  ToolsetCronjob,

	"rca_grep":     ToolsetRCA,
	"rca_glob":     ToolsetRCA,
	"rca_read":     ToolsetRCA,
	"rca_symbol":   ToolsetRCA,
	"jaeger_trace": ToolsetRCA,
	"es_log_query": ToolsetRCA,

	"browser_navigate":   ToolsetBrowser,
	"browser_snapshot":   ToolsetBrowser,
	"browser_click":      ToolsetBrowser,
	"browser_type":       ToolsetBrowser,
	"browser_scroll":     ToolsetBrowser,
	"browser_back":       ToolsetBrowser,
	"browser_press":      ToolsetBrowser,
	"browser_get_images": ToolsetBrowser,
	"browser_console":    ToolsetBrowser,
	"browser_vision":     ToolsetBrowser,
	"browser_cdp":        ToolsetBrowser,
	"browser_dialog":     ToolsetBrowser,
	"vision_analyze":     ToolsetBrowser,

	// 演示用算数工具：Hermes 无直接对应，暂归入 skills 侧车能力。
	"calculator_add": ToolsetSkills,
}

// ListByToolsets 返回 Tool.Toolset 属于 toolsets 之一的所有已注册工具（顺序与 List 一致）。
// toolsets 为空或 nil 时等价于 List()（不过滤）。
// 未设置 Toolset 且不在 builtinDefaultToolset 中的工具不会出现在过滤结果中；全量请用 List()。
func (r *Registry) ListByToolsets(toolsets []string) []Tool {
	if len(toolsets) == 0 {
		return r.List()
	}
	want := make(map[string]struct{}, len(toolsets))
	for _, s := range toolsets {
		if s != "" {
			want[s] = struct{}{}
		}
	}
	if len(want) == 0 {
		return r.List()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		if t.Toolset == "" {
			continue
		}
		if _, ok := want[t.Toolset]; ok {
			out = append(out, t)
		}
	}
	return out
}
