package growth

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/sixath/framework/skills"
)

// FormatSkillsIndexSnapshot 将技能索引压成短文本，供复盘 LLM / 假 LLM 上下文（路径 + 摘要）。
// maxSkills 为最多列出的技能条数；maxDescRunes 为每条 description 截断上限（按 rune）。
func FormatSkillsIndexSnapshot(idx *skills.Index, maxSkills, maxDescRunes int) string {
	if idx == nil || maxSkills <= 0 {
		return ""
	}
	all := idx.All()
	if len(all) == 0 {
		return "(no skills indexed)\n"
	}
	if len(all) > maxSkills {
		all = all[:maxSkills]
	}
	var b strings.Builder
	b.WriteString("# Skills index snapshot\n\n")
	for _, m := range all {
		desc := m.Description
		if maxDescRunes > 0 && utf8.RuneCountInString(desc) > maxDescRunes {
			runes := []rune(desc)
			desc = string(runes[:maxDescRunes]) + "…"
		}
		fmt.Fprintf(&b, "- **%s** — %s\n  path: `%s`\n", m.Name, desc, m.Path)
	}
	return b.String()
}
