package chat

import (
	"os"
	"strings"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
)

const (
	FamilyCore           = "core"
	FamilyRCA            = "rca"
	FamilyWeb            = "web"
	FamilyKnowledge      = "knowledge"
	turnToolSurfaceEnv   = "SATH_TURN_TOOL_SURFACE"
)

func ToolSurfaceEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(turnToolSurfaceEnv)))
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
	"jaeger_trace":  FamilyRCA,
	"es_log_query":  FamilyRCA,
	"rca_grep":      FamilyRCA,
	"rca_glob":      FamilyRCA,
	"rca_read":      FamilyRCA,
	"rca_symbol":    FamilyRCA,
	"web_search":    FamilyWeb,
	"web_extract":   FamilyWeb,
	"knowledge_search":  FamilyKnowledge,
	"knowledge_read":    FamilyKnowledge,
	"knowledge_write":   FamilyKnowledge,
	"knowledge_approve": FamilyKnowledge,
}

// familyKeywords: family → aliases (lowercase). MCP families also match server id/name at resolve time.
var familyKeywords = map[string][]string{
	FamilyRCA:       {"jaeger", "trace", "span", "opentelemetry", "otel", "es_log", "elasticsearch", "日志排查", "链路"},
	FamilyWeb:       {"联网", "搜索网页", "web_search", "http://", "https://"},
	FamilyKnowledge: {"wiki", "knowledge", "知识库", "文档库"},
}

func FamilyForBuiltinToolName(name string) string {
	if f, ok := builtinToolFamily[strings.TrimSpace(name)]; ok {
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
			set[FamilyRCA] = struct{}{}
		case biz.ToolTypeMCP:
			mc := tool.McpConfigFromMap(toolConfigToMap(t.Config))
			if mc != nil && mc.Id != "" {
				set[MCPFamilyID(mc.Id)] = struct{}{}
			} else {
				set[LegacyMCPFamilyID(t.Name)] = struct{}{}
			}
		case biz.ToolTypeDatasource, biz.ToolTypeBuiltin:
			// phase-1: treat as core (always allowed when bound)
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
