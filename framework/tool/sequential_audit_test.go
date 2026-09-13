package tool

import "testing"

// 并行执行的审计表（Task 6）：启用 ParallelTools 前必须确认「有副作用 / 交互 / 共享会话态」
// 的工具都被标记为 RequiresSequential。本用例把审计结论固化下来，防止后续新增工具被漏标。
func TestBuiltinRequiresSequential_Audit(t *testing.T) {
	mustBeSequential := []string{
		// 写操作
		"write_file", "patch", "execute_write", "skill_manage", "append_learning",
		// 人机交互 / 会话态
		"ask_user", "todo", "cronjob",
		// 命令与脚本执行
		"terminal", "process", "ssh_exec", "scp", "execute_skill_script", "hypertool",
		// 共享浏览器会话
		"browser_navigate", "browser_snapshot", "browser_click", "browser_type",
		"browser_scroll", "browser_back", "browser_press", "browser_get_images",
		"browser_console", "browser_vision", "browser_cdp", "browser_dialog",
		"vision_analyze",
		// 出站消息（限流 + 顺序可见）
		"send_to_wecom",
	}
	for _, name := range mustBeSequential {
		if !builtinRequiresSequential[name] {
			t.Fatalf("%q must be marked RequiresSequential before enabling ParallelTools by default", name)
		}
	}

	// 只读工具不应被串行化，否则并行收益会被无谓地吃掉。
	mustStayParallelSafe := []string{
		"read_file", "search_files", "list_tables", "describe_table", "execute_read",
		"web_search", "web_extract", "http_request", "list_tools", "tool_search",
		"tool_describe", "memory_recall", "memory_get", "rca_grep", "rca_glob", "rca_read",
	}
	for _, name := range mustStayParallelSafe {
		if builtinRequiresSequential[name] {
			t.Fatalf("%q is read-only and should stay parallel-safe", name)
		}
	}
}
