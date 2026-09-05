package skills

import (
	"fmt"
	"strings"
)

// BuildSkillsSummary 根据 Skill 列表构造一段简短的系统提示摘要。
// maxCount <= 0 时默认展示最多 8 个 Skill。
func BuildSkillsSummary(all []SkillMeta, maxCount int) string {
	if len(all) == 0 {
		return ""
	}
	if maxCount <= 0 {
		maxCount = 8
	}
	if len(all) > maxCount {
		all = all[:maxCount]
	}
	var b strings.Builder
	b.WriteString("【可用 Skills（按需加载）】\n")
	b.WriteString("你可以按需加载以下技能以增强能力：\n")
	b.WriteString("- 使用 `load_skill` / `read_skill_file` / `execute_skill_script` 这三个工具与 Skills 交互。\n")
	b.WriteString("- 列表中的 **Skill 名称只是参数，不是工具名称**，不要尝试直接调用它们作为工具。\n")
	b.WriteString("当你需要使用某个 Skill 时，请先调用 `load_skill`，在阅读完整 SKILL.md 后，再按其中说明调用其它工具或继续推理。同一 Skill 每轮最多加载一次；若已自动匹配或工具返回 already loaded，禁止再次 load_skill / skill_view，应立刻用其它工具继续工作流。\n")
	for _, m := range all {
		desc := strings.TrimSpace(m.Description)
		if desc == "" {
			desc = "(无描述)"
		}
		b.WriteString(fmt.Sprintf("- %s：%s\n", m.Name, desc))
	}
	return b.String()
}

// BuildSkillsAwarePrompt 构造带 Skills 摘要的系统提示。
// 返回完整的技能使用说明，包含 load_skill/read_skill_file/execute_skill_script 的用法与可用技能列表。
func BuildSkillsAwarePrompt(skillsIdx *Index) string {
	var b strings.Builder
	b.WriteString("你是一个具备 Skills 能力的通用对话助手。\n")
	b.WriteString("Skills 以 SKILL.md 文件的形式提供特定领域或任务的操作手册。\n")
	b.WriteString("注意：Skill 的名称（如 mysql-employees-analysis）不是工具名，不能直接当作工具调用；你只能调用显式提供的工具，比如 `load_skill` / `read_skill_file` / `execute_skill_script`。\n")
	b.WriteString("当你判断与某个 Skill 相关时：若 system 中已有【已自动匹配 Skill】正文，不要再 load_skill；手册与用户问题冲突时以用户问题为准。需要细节时再调用一次 `load_skill(name)`。同一 Skill 每轮最多加载一次。\n")
	b.WriteString("执行策略：不要仅凭推断就拒绝执行。未加载且未自动匹配时先 load_skill 一次，再根据技能说明决定下一步。若 load_skill / skill_view 返回 already loaded，立即改用其它工具，禁止反复加载同一技能。若技能支持可选参数或默认值，应尝试执行；仅当技能明确要求必填参数且你确实无法获取时，再向用户说明需要提供哪些信息。不要在一开始就罗列所有可能参数并等待用户填写。\n")
	b.WriteString("重要：当你缺少必要信息、无法访问外部系统或脚本执行被禁用时，不要凭空编造具体结果（例如版本号、数量、精确日志内容等）。此时应如实说明受限原因，可以给出一般性的排查建议，但必须明确这不是基于真实执行结果的结论。\n")
	b.WriteString("关于脚本执行：不要事先假定「脚本执行已禁用」。仅当你实际调用了 `execute_skill_script` 且工具明确返回了脚本被禁用的错误信息时，才向用户说明需要开启 skills.allow_script_execution；未调用工具前不得提前给出此类结论。\n\n")

	if skillsIdx != nil {
		summary := BuildSkillsSummary(skillsIdx.All(), 8)
		if summary != "" {
			b.WriteString(summary)
			b.WriteString("\n")
		}
	}

	b.WriteString("使用建议：Skills 是可选手册。任务与已知 Skill 相关且尚未出现在上下文时，可调用一次 load_skill(name) 获取细节；不要反复加载同一技能。\n")
	b.WriteString("排障结束后，若得到可复用的修正或经验，请调用 append_learning 写入 .learnings，以便后续自动沉淀为 Skill。\n")
	return b.String()
}
