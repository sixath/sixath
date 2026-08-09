package skills

import (
	"strings"
	"testing"
)

func TestValidateSkillMarkdown_RequiresDescription(t *testing.T) {
	content := "---\nname: my-skill\n---\n# Body\n"
	_, _, err := ValidateSkillMarkdown(content, "my-skill")
	if err == nil || !strings.Contains(err.Error(), "description") {
		t.Fatalf("want description error, got %v", err)
	}
}

func TestValidateSkillMarkdown_NameMustMatchParam(t *testing.T) {
	content := "---\nname: other\ndescription: Use when testing name mismatch for skills.\n---\n# Body with enough runes for later quality checks maybe\n"
	_, _, err := ValidateSkillMarkdown(content, "my-skill")
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("want name mismatch error, got %v", err)
	}
}

func TestValidateSkillMarkdown_OK(t *testing.T) {
	content := "---\nname: my-skill\ndescription: Use when validating the happy path for skill schema.\n---\n# My Skill\n\nSteps and success checklist here with ok: true signal.\n"
	meta, body, err := ValidateSkillMarkdown(content, "my-skill")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "my-skill" || meta.Description == "" {
		t.Fatalf("meta: %#v", meta)
	}
	if !strings.Contains(body, "# My Skill") {
		t.Fatalf("body: %q", body)
	}
}

func TestValidateSkillMarkdown_TableDriven(t *testing.T) {
	cases := []struct {
		name       string
		content    string
		expectName string
		wantSubstr string
	}{
		{
			name:       "missing frontmatter",
			content:    "# Just a heading\n\nNo YAML here.\n",
			expectName: "my-skill",
			wantSubstr: "frontmatter",
		},
		{
			name:       "illegal kebab",
			content:    "---\nname: My_Skill\ndescription: Use when testing illegal kebab-case names.\n---\n# Body\n",
			expectName: "My_Skill",
			wantSubstr: "kebab",
		},
		{
			name:       "bad YAML",
			content:    "---\nname: my-skill\ndescription: [unclosed\n---\n# Body\n",
			expectName: "my-skill",
			wantSubstr: ErrCodeSkillSchemaInvalid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ValidateSkillMarkdown(tc.content, tc.expectName)
			if err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("want error containing %q, got %v", tc.wantSubstr, err)
			}
		})
	}
}

func TestAssessSkillQuality_DescTooShortAndNotTrigger(t *testing.T) {
	meta := SkillMeta{Name: "x", Description: "short"}
	body := "# Hi"
	ws := AssessSkillQuality(meta, body)
	codes := map[string]bool{}
	for _, w := range ws {
		codes[w.Code] = true
	}
	for _, want := range []string{"desc_too_short", "desc_not_trigger", "body_too_short", "no_success_signal"} {
		if !codes[want] {
			t.Fatalf("missing %s in %#v", want, ws)
		}
	}
}

func TestAssessSkillQuality_GoodSampleQuiet(t *testing.T) {
	meta := SkillMeta{
		Name:        "rca-investigation",
		Description: "Use when diagnosing production failures with RCA tools and traces.",
	}
	body := strings.Repeat("步骤与验收 checklist，成功信号 ok: true。\n", 20)
	ws := AssessSkillQuality(meta, body)
	if len(ws) != 0 {
		t.Fatalf("want no warnings, got %#v", ws)
	}
}
