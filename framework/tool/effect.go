package tool

import "strings"

// Effect 描述一次工具调用对外部系统的副作用级别。仅作提示用途（收尾关卡、审计展示），
// 不替代 PermissionPolicy / 确认 token 等真正的权限控制。
type Effect string

const (
	// EffectUnknown 未标注；调用方应按「可能有副作用」保守处理。
	EffectUnknown Effect = ""
	// EffectRead 只读：查询、列目录、读文件、读会话内状态。
	EffectRead Effect = "read"
	// EffectWrite 会改变外部系统或持久状态：写库、发消息、杀进程、改配置等。
	EffectWrite Effect = "write"
	// EffectExec 任意命令执行，副作用取决于命令本身，无法静态判定。
	EffectExec Effect = "exec"
)

// builtinDefaultEffect 为内置工具名到默认副作用级别的映射；Register 时若 Tool.Effect 为空则自动填入。
var builtinDefaultEffect = map[string]Effect{
	"execute_read":   EffectRead,
	"execute_write":  EffectWrite,
	"list_tables":    EffectRead,
	"describe_table": EffectRead,
	"read_file":      EffectRead,
	"search_files":   EffectRead,
	"write_file":     EffectWrite,
	"patch":          EffectWrite,
	"result_stats":   EffectRead,
	"web_search":     EffectRead,
	"web_extract":    EffectRead,

	"load_skill":           EffectRead,
	"read_skill_file":      EffectRead,
	"skills_list":          EffectRead,
	"skill_view":           EffectRead,
	"skill_manage":         EffectWrite,
	"execute_skill_script": EffectExec,

	"memory_recall":   EffectRead,
	"memory_get":      EffectRead,
	"memory_search":   EffectRead,
	"memory_remember": EffectWrite,
	"todo":            EffectRead,
	"list_tools":      EffectRead,
	"tool_search":     EffectRead,

	"ssh_exec": EffectExec,
	"scp":      EffectWrite,
	"terminal": EffectExec,
	"process":  EffectExec,
	"cronjob":  EffectWrite,

	"rca_grep":         EffectRead,
	"rca_glob":         EffectRead,
	"rca_read":         EffectRead,
	"rca_symbol":       EffectRead,
	"jaeger_trace":     EffectRead,
	"es_log_query":     EffectRead,
	"deep_investigate": EffectRead,

	"browser_snapshot":   EffectRead,
	"browser_get_images": EffectRead,
	"browser_console":    EffectRead,
	"browser_vision":     EffectRead,
	"vision_analyze":     EffectRead,
}

// EffectFor 返回某次调用的副作用级别：EffectFn 优先，其次静态 Effect。
func (t Tool) EffectFor(args map[string]any) Effect {
	if t.EffectFn != nil {
		if e := t.EffectFn(args); e != EffectUnknown {
			return e
		}
	}
	return t.Effect
}

// HTTPMethodEffect 按 HTTP 方法判定副作用：GET/HEAD/OPTIONS 为只读，其余为写。
// 缺省 method 按 GET 只读；但带 body 时意图不明（多半想 POST），按写处理以触发审批。
func HTTPMethodEffect(args map[string]any) Effect {
	m, _ := args["method"].(string)
	switch strings.ToUpper(strings.TrimSpace(m)) {
	case "":
		if b, _ := args["body"].(string); b != "" {
			return EffectWrite
		}
		return EffectRead
	case "GET", "HEAD", "OPTIONS":
		return EffectRead
	default:
		return EffectWrite
	}
}
