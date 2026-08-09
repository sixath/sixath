package skills

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var skillNameKebab = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const (
	ErrCodeSkillSchemaInvalid = "skill_schema_invalid"
	SkillSchemaHint           = "SKILL.md must start with YAML frontmatter containing name and description."
	SkillSchemaExample        = "---\nname: my-skill\ndescription: >\n  Use when …\n---\n\n# My Skill\n"

	descTooShortRunes = 40
	bodyTooShortRunes = 120
)

var successSignalSubstrs = []string{"成功", "success", "checklist", "验收", "ok:"}

type SkillWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidateSkillMarkdown enforces H1–H6 for skill_manage writes.
func ValidateSkillMarkdown(content, expectName string) (SkillMeta, string, error) {
	content = strings.TrimLeft(content, "\ufeff")
	meta, ok, err := parseSkillFrontmatterContent(content, "SKILL.md")
	if err != nil {
		return SkillMeta{}, "", fmt.Errorf("%s: %w", ErrCodeSkillSchemaInvalid, err)
	}
	if !ok {
		return SkillMeta{}, "", fmt.Errorf("%s: missing YAML frontmatter", ErrCodeSkillSchemaInvalid)
	}
	name := strings.TrimSpace(meta.Name)
	expectName = strings.TrimSpace(expectName)
	if name == "" {
		return SkillMeta{}, "", fmt.Errorf("%s: name is required", ErrCodeSkillSchemaInvalid)
	}
	if !skillNameKebab.MatchString(name) {
		return SkillMeta{}, "", fmt.Errorf("%s: name must be kebab-case", ErrCodeSkillSchemaInvalid)
	}
	if name != expectName {
		return SkillMeta{}, "", fmt.Errorf("%s: frontmatter name %q != param name %q", ErrCodeSkillSchemaInvalid, name, expectName)
	}
	if strings.TrimSpace(meta.Description) == "" {
		return SkillMeta{}, "", fmt.Errorf("%s: description is required", ErrCodeSkillSchemaInvalid)
	}
	parts := strings.SplitN(content, "---", 3)
	body := ""
	if len(parts) >= 3 {
		body = parts[2]
	}
	return meta, body, nil
}

// AssessSkillQuality returns soft warnings for skill content quality.
// It never returns an error — only []SkillWarning.
func AssessSkillQuality(meta SkillMeta, body string) []SkillWarning {
	var warnings []SkillWarning
	desc := strings.TrimSpace(meta.Description)

	if utf8.RuneCountInString(desc) < descTooShortRunes {
		warnings = append(warnings, SkillWarning{
			Code:    "desc_too_short",
			Message: fmt.Sprintf("description should be at least %d characters (runes)", descTooShortRunes),
		})
	}
	if !hasTriggerPrefix(desc) {
		warnings = append(warnings, SkillWarning{
			Code:    "desc_not_trigger",
			Message: `description should start with a trigger phrase ("Use when", "使用时机", or "当")`,
		})
	}
	if utf8.RuneCountInString(body) < bodyTooShortRunes {
		warnings = append(warnings, SkillWarning{
			Code:    "body_too_short",
			Message: fmt.Sprintf("body should be at least %d characters (runes)", bodyTooShortRunes),
		})
	}
	if !hasSuccessSignal(body) {
		warnings = append(warnings, SkillWarning{
			Code:    "no_success_signal",
			Message: "body should include a success signal (成功, success, checklist, 验收, or ok:)",
		})
	}
	return warnings
}

func hasTriggerPrefix(desc string) bool {
	if strings.HasPrefix(strings.ToLower(desc), "use when") {
		return true
	}
	if strings.HasPrefix(desc, "使用时机") {
		return true
	}
	if strings.HasPrefix(desc, "当") {
		return true
	}
	return false
}

func hasSuccessSignal(body string) bool {
	lower := strings.ToLower(body)
	for _, s := range successSignalSubstrs {
		if strings.Contains(lower, strings.ToLower(s)) {
			return true
		}
	}
	return false
}
