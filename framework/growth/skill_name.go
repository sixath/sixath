package growth

import (
	"strings"

	yaml "go.yaml.in/yaml/v2"
)

// ParseSkillNameFromMarkdown 从 SKILL.md 正文解析 frontmatter 中的 name 字段。
func ParseSkillNameFromMarkdown(content string) (string, bool) {
	content = strings.TrimLeft(content, "\ufeff")
	if !strings.HasPrefix(content, "---") {
		return "", false
	}
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return "", false
	}
	var fm struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		return "", false
	}
	name := strings.TrimSpace(fm.Name)
	if name == "" {
		return "", false
	}
	return name, true
}

func isSkillMarkdownPath(path string) bool {
	return strings.EqualFold(strings.TrimSpace(path), "SKILL.md") ||
		strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), "/skill.md")
}
