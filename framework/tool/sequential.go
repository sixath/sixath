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

	"terminal": true,
	"process":  true,
	"ssh_exec": true,
	"scp":      true,

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
