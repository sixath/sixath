package tool

// builtinRequiresSequential marks tools that must not share a parallel step with others
// (writes, interactive confirm, shared browser/terminal session state).
// Unknown / omitted names default to false (parallel-safe when ParallelTools is on).
var builtinRequiresSequential = map[string]bool{
	"write_file":    true,
	"patch":         true,
	"execute_write": true,
	"skill_manage":  true,
	"ask_user":      true,
	"todo":          true,
	"cronjob":       true,

	// 本地/远端命令与脚本执行：与彼此或写操作并行会造成竞态与不可预测的副作用。
	"terminal": true,
	"process":  true,
	"ssh_exec": true,
	"scp":      true,
	// 技能脚本与 hypertool 可执行任意命令/内部工具调用（等同 terminal）。
	"execute_skill_script": true,
	"hypertool":            true,
	// 追加写同一文件：并行 append 可能交错。
	"append_learning": true,

	// 出站消息有会话级限流且顺序对用户可见，串行发送避免 429 与乱序。
	"send_to_wecom": true,

	"browser_navigate":   true,
	"browser_snapshot":   true,
	"browser_click":      true,
	"browser_type":       true,
	"browser_scroll":     true,
	"browser_back":       true,
	"browser_press":      true,
	"browser_get_images": true,
	"browser_console":    true,
	"browser_vision":     true,
	"browser_cdp":        true,
	"browser_dialog":     true,
	"vision_analyze":     true,
}
