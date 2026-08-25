package templates

import (
	"strings"
	"testing"
)

func TestBuildSkillsAwarePrompt_noWorkflowAuthority(t *testing.T) {
	got := BuildSkillsAwarePrompt(nil)
	if strings.Contains(got, "直接按该工作流执行") {
		t.Fatalf("catalog must not command executing the matched workflow: %s", got)
	}
	if strings.Contains(got, "严格遵循") {
		t.Fatalf("catalog must not require strictly following a skill: %s", got)
	}
}
