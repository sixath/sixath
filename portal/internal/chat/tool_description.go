package chat

import (
	"strings"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
)

// portalToolDescriptionMaxRunes 限制单条部署说明长度，避免挤占工具 schema 预算。
const portalToolDescriptionMaxRunes = 800

// rcaFuncPathToolNames 为 RCA func_path 到其实际注册的框架工具名。
var rcaFuncPathToolNames = map[string][]string{
	"rca_code":     {"rca_grep", "rca_glob", "rca_read"},
	"rca_symbol":   {"rca_symbol"},
	"jaeger_trace": {"jaeger_trace"},
	"es_log_query": {"es_log_query"},
	"vm_run_cmd":   {"vm_run_cmd"},
}

// applyPortalToolDescriptions 把 Portal 工具配置里的描述作为「部署说明」追加到对应框架工具上。
// 框架内置描述负责参数语义，部署说明补充端口、用途、环境等只有部署方知道的信息。
func applyPortalToolDescriptions(reg *tool.Registry, tools []*biz.ToolMeta) {
	if reg == nil {
		return
	}
	for _, t := range tools {
		if t == nil || t.Type != biz.ToolTypeRCA {
			continue
		}
		desc := truncateRunes(strings.TrimSpace(t.Description), portalToolDescriptionMaxRunes)
		if desc == "" {
			continue
		}
		cfg := toolConfigToMap(t.Config)
		rcaMap, _ := cfg["rca"].(map[string]interface{})
		funcPath, _ := rcaMap["func_path"].(string)
		extra := "部署说明"
		if name := strings.TrimSpace(t.Name); name != "" {
			extra += "（" + name + "）"
		}
		extra += "：" + desc
		for _, name := range rcaFuncPathToolNames[funcPath] {
			reg.AppendDescription(name, extra)
		}
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
