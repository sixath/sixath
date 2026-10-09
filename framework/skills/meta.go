package skills

import "strings"

// SkillMeta 描述一个 Skill 的基础元数据，由 SKILL.md 的 frontmatter 解析而来。
type SkillMeta struct {
	// Name 是 Skill 的唯一标识，推荐使用 kebab-case（例如 "frontend-design"）。
	Name string
	// Description 为简要描述，说明 Skill 做什么、何时使用。
	Description string
	// Tags 用于按领域或场景过滤 Skill（如 "database"、"frontend" 等）。
	Tags []string
	// Scopes 指定此 Skill 适用的 Agent 范围，如 "chat"、"dataquery"、"mcp" 等。
	Scopes []string
	// AllowedTools 声明此 Skill 期望或允许使用的工具列表，便于执行层做安全控制。
	AllowedTools []string
	// Path 为 SKILL.md 在文件系统中的绝对或相对路径，用于后续加载正文。
	Path string
	// MCPServers 该 Skill 依赖的 MCP 服务 ID 列表，与全局配置中的 MCP 服务对应。
	MCPServers []string
	// MCPTools 该 Skill 允许调用的 MCP 工具名列表（可选，用于白名单或提示）。
	MCPTools []string
	// HiddenFromSummary 为 true 时不进系统提示摘要、不参与自动路由和 skills_list，
	// 只能按名字 load_skill / skill_view / read_skill_file（仓库 handbook 使用）。
	HiddenFromSummary bool
	// SummaryPinned 为 true 时在系统提示摘要中排在最前，不会被条数上限截掉。
	SummaryPinned bool
}

// VisibleSkills returns the skills not marked hidden_from_summary, in their original order.
func VisibleSkills(all []SkillMeta) []SkillMeta {
	out := make([]SkillMeta, 0, len(all))
	for _, m := range all {
		if !m.HiddenFromSummary {
			out = append(out, m)
		}
	}
	return out
}

// IsReservedSkillName reports whether name belongs to generated repository handbooks
// ("code-map" and the "handbook-" prefix); user-authored skills must not use it.
func IsReservedSkillName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "code-map" || strings.HasPrefix(n, "handbook-")
}
