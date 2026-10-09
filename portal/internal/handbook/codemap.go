package handbook

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// CodeMapSkillName is the per-agent entry skill listing its repositories and handbooks.
const CodeMapSkillName = "code-map"

// CodeMapEntry is one effective repository of an agent.
type CodeMapEntry struct {
	RelPath     string
	Description string
	SubPaths    []string
	SkillName   string // empty when the repository has no handbook yet
}

// RenderCodeMap renders the code-map SKILL.md; entries should be sorted by RelPath.
func RenderCodeMap(entries []CodeMapEntry) string {
	groups := map[string][]CodeMapEntry{}
	var keys []string
	for _, e := range entries {
		k := path.Dir(e.RelPath)
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], e)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\ndescription: 你可访问的 %d 个代码仓库及其 handbook。排查与代码相关的问题时先读它，再按仓库查看 handbook。\nsummary_pinned: true\n---\n\n", CodeMapSkillName, len(entries))
	b.WriteString("# 代码仓库目录\n\nrca_* 工具的 repo 参数使用下面的仓库逻辑名。有 handbook 的仓库先 `skill_view` 它，再用 `rca_read` 核实源码。\n")
	if len(entries) == 0 {
		b.WriteString("\n当前没有可访问的仓库。\n")
		return b.String()
	}
	for _, k := range keys {
		title := k
		if k == "." {
			title = "（顶层）"
		}
		es := groups[k]
		fmt.Fprintf(&b, "\n## %s（%d 个）\n\n", title, len(es))
		for _, e := range es {
			fmt.Fprintf(&b, "- `%s`", e.RelPath)
			if d := firstLine(e.Description); d != "" {
				b.WriteString("：" + d)
			}
			if len(e.SubPaths) > 0 {
				names := make([]string, len(e.SubPaths))
				for i, sp := range e.SubPaths {
					names[i] = "`" + e.RelPath + "/" + sp + "`"
				}
				b.WriteString("（仅子目录，repo 参数用 " + strings.Join(names, "、") + "）")
			}
			if e.SkillName != "" {
				fmt.Fprintf(&b, " —— skill_view(\"%s\")\n", e.SkillName)
			} else {
				b.WriteString(" —— 暂无 handbook，直接用 rca_grep\n")
			}
		}
	}
	return b.String()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80]) + "…"
	}
	return s
}
