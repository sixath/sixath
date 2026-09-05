package chat

import (
	"os"
	"strings"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
)

const (
	FamilyCore         = "core"
	FamilyCode         = "code"
	FamilyRCA          = "rca"
	FamilyWeb          = "web"
	FamilyKnowledge    = "knowledge"
	FamilyData         = "data"
	FamilySkills       = "skills"
	FamilyMemory       = "memory"
	toolFamilySplitEnv = "SATH_TOOL_FAMILY_SPLIT"
)

// ToolFamilySplitEnabled 为 true 时 data/skills/memory 独立成族（默认开）。
// SATH_TOOL_FAMILY_SPLIT=0 回退 8-09：这些工具仍算 core。
func ToolFamilySplitEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(toolFamilySplitEnv)))
	if v == "" {
		return true
	}
	return !(v == "0" || v == "false" || v == "off" || v == "no")
}

func MCPFamilyID(serverID string) string {
	return "mcp:" + strings.TrimSpace(serverID)
}

func LegacyMCPFamilyID(toolID string) string {
	return "mcp:legacy:" + strings.TrimSpace(toolID)
}

// builtinToolFamily maps known non-MCP tool names → family.
var builtinToolFamily = map[string]string{
	"jaeger_trace":         FamilyRCA,
	"es_log_query":         FamilyRCA,
	"rca_grep":             FamilyCode,
	"rca_glob":             FamilyCode,
	"rca_read":             FamilyCode,
	"rca_symbol":           FamilyCode,
	"http_request":         FamilyCore,
	"ask_user":             FamilyCore,
	"todo":                 FamilyCore,
	"web_search":           FamilyWeb,
	"web_extract":          FamilyWeb,
	"knowledge_search":     FamilyKnowledge,
	"knowledge_read":       FamilyKnowledge,
	"knowledge_write":      FamilyKnowledge,
	"knowledge_approve":    FamilyKnowledge,
	"list_tables":          FamilyData,
	"describe_table":       FamilyData,
	"execute_read":         FamilyData,
	"execute_write":        FamilyData,
	"skill_view":           FamilySkills,
	"load_skill":           FamilySkills,
	"skills_list":          FamilySkills,
	"read_skill_file":      FamilySkills,
	"execute_skill_script": FamilySkills,
	"skill_manage":         FamilySkills,
	"memory_recall":        FamilyMemory,
	"memory_search":        FamilyMemory,
	"memory_get":           FamilyMemory,
	"memory_remember":      FamilyMemory,
	"session_search":       FamilyMemory,
}

// familyKeywords: family → aliases (lowercase). MCP families also match server id/name at resolve time.
var familyKeywords = map[string][]string{
	FamilyCode:      {"源码", "代码分析", "代码", "调用链", "模块关系", "流程梳理", "谁调用", "仓库", "grep", "go.mod"},
	FamilyRCA:       {"jaeger", "trace", "span", "opentelemetry", "otel", "es_log", "elasticsearch", "日志排查", "链路"},
	FamilyWeb:       {"联网", "搜索网页", "web_search", "http://", "https://"},
	FamilyKnowledge: {"wiki", "knowledge", "知识库", "文档库"},
	FamilyData:      {"查库", "查表", "集合", "mongo", "mongodb", "mysql", "sql", "实际数据", "有哪些记录", "线上数据", "这条数据"},
	FamilySkills:    {"skill", "技能", "手册", "按技能", "load_skill", "skill_view"},
	FamilyMemory:    {"上次", "记忆", "我们讨论过", "之前说过", "session 里"},
}

func FamilyForBuiltinToolName(name string) string {
	if f, ok := builtinToolFamily[strings.TrimSpace(name)]; ok {
		if !ToolFamilySplitEnabled() && (f == FamilyData || f == FamilySkills || f == FamilyMemory) {
			return FamilyCore
		}
		return f
	}
	return FamilyCore
}

func FamilyForRegisteredTool(tl tool.Tool) string {
	if tl.Bindings != nil {
		if sid := strings.TrimSpace(tl.Bindings["mcp_server"]); sid != "" {
			return MCPFamilyID(sid)
		}
	}
	return FamilyForBuiltinToolName(tl.Name)
}

func BoundFamiliesFrom(tools []*biz.ToolMeta, servers []*biz.McpServerMeta, webEnabled, knowledgeEnabled bool) []string {
	set := map[string]struct{}{FamilyCore: {}}
	for _, s := range servers {
		if s == nil || s.ID == "" {
			continue
		}
		set[MCPFamilyID(s.ID)] = struct{}{}
	}
	for _, t := range tools {
		if t == nil {
			continue
		}
		switch t.Type {
		case biz.ToolTypeRCA:
			set[familyForRCATool(t)] = struct{}{}
		case biz.ToolTypeMCP:
			mc := tool.McpConfigFromMap(toolConfigToMap(t.Config))
			if mc != nil && mc.Id != "" {
				set[MCPFamilyID(mc.Id)] = struct{}{}
			} else {
				set[LegacyMCPFamilyID(t.Name)] = struct{}{}
			}
		case biz.ToolTypeDatasource:
			if isElasticsearchType(datasourceTypeFromMeta(t)) {
				set[FamilyRCA] = struct{}{}
				continue
			}
			if ToolFamilySplitEnabled() {
				set[FamilyData] = struct{}{}
			}
		case biz.ToolTypeBuiltin:
			// runtime builtins (todo / files) stay core; split families are registered at runtime
		}
	}
	if webEnabled {
		set[FamilyWeb] = struct{}{}
	}
	if knowledgeEnabled {
		set[FamilyKnowledge] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

// familyForRCATool maps a bound RCA tool to code vs rca by rca.func_path.
// Unknown or missing path stays on FamilyRCA for backward compatibility.
func familyForRCATool(t *biz.ToolMeta) string {
	if t == nil {
		return FamilyRCA
	}
	switch rcaFuncPath(t) {
	case "rca_code", "rca_symbol":
		return FamilyCode
	default:
		return FamilyRCA
	}
}

func rcaFuncPath(t *biz.ToolMeta) string {
	if t == nil {
		return ""
	}
	cfg := toolConfigToMap(t.Config)
	if cfg == nil {
		return ""
	}
	rcaMap, _ := cfg["rca"].(map[string]interface{})
	if rcaMap == nil {
		return ""
	}
	fp, _ := rcaMap["func_path"].(string)
	return strings.TrimSpace(fp)
}

func datasourceTypeFromMeta(t *biz.ToolMeta) string {
	if t == nil || t.Config == nil {
		return ""
	}
	return mapStringField(datasourceMapFromTool(t), "type")
}

func familySet(ids []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

func FamilyActive(active map[string]struct{}, family string) bool {
	if active == nil {
		return true
	}
	_, ok := active[family]
	return ok
}

// InferPrimaryFamilies 低置信 Fail-narrow 用的主族：已绑定的 code/rca；二者都无则仅 data。
func InferPrimaryFamilies(bound []string) []string {
	s := familySet(bound)
	var out []string
	if _, ok := s[FamilyCode]; ok {
		out = append(out, FamilyCode)
	}
	if _, ok := s[FamilyRCA]; ok {
		out = append(out, FamilyRCA)
	}
	if len(out) == 0 {
		if _, ok := s[FamilyData]; ok {
			out = append(out, FamilyData)
		}
	}
	return out
}

func mergeFamilyIDs(ids []string, extra ...string) []string {
	s := familySet(ids)
	for _, e := range extra {
		if strings.TrimSpace(e) == "" {
			continue
		}
		s[e] = struct{}{}
	}
	out := make([]string, 0, len(s))
	for id := range s {
		out = append(out, id)
	}
	return out
}

// BuildToolFamilyIndex maps registered tool names to family ids for TurnIntentGate.
func BuildToolFamilyIndex(reg *tool.Registry) map[string]string {
	out := map[string]string{}
	if reg == nil {
		return out
	}
	for _, tl := range reg.List() {
		out[tl.Name] = FamilyForRegisteredTool(tl)
	}
	return out
}
