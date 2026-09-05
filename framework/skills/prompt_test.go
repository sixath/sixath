package skills

import (
	"strings"
	"testing"
)

func TestBuildSkillsSummary_empty(t *testing.T) {
	if got := BuildSkillsSummary(nil, 8); got != "" {
		t.Fatalf("nil: got %q", got)
	}
	if got := BuildSkillsSummary([]SkillMeta{}, 8); got != "" {
		t.Fatalf("empty: got %q", got)
	}
}

func TestBuildSkillsAwarePrompt_nilIndexHasNoSkillList(t *testing.T) {
	out := BuildSkillsAwarePrompt(nil)
	if !strings.Contains(out, "你是一个具备 Skills 能力的通用对话助手。") {
		t.Fatalf("missing header: %q", out)
	}
	if strings.Contains(out, "【可用 Skills") {
		t.Fatalf("nil index should omit skills list")
	}
}
