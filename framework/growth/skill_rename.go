package growth

// ExtractSkillRenamesFromPatches 从 patch 批次中提取 frontmatter name 变更（old→new）。
// 仅处理 SKILL.md 上的 OpPatch；删除/新建不产生映射。
func ExtractSkillRenamesFromPatches(batch []Patch) map[string]string {
	out := make(map[string]string)
	for i := range batch {
		p := batch[i]
		if p.Op != OpPatch || !isSkillMarkdownPath(p.Path) {
			continue
		}
		oldName, okOld := ParseSkillNameFromMarkdown(p.Old)
		newName, okNew := ParseSkillNameFromMarkdown(p.New)
		if !okOld || !okNew || oldName == newName {
			continue
		}
		out[oldName] = newName
	}
	return out
}
