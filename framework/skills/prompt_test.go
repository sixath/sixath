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

func TestBuildSkillsAwarePrompt_omitsAppendLearning(t *testing.T) {
	out := BuildSkillsAwarePrompt(nil)
	if strings.Contains(out, "append_learning") {
		t.Fatal("skills prompt must not teach append_learning")
	}
}

func TestBuildSkillsAwarePrompt_errorQuoteUsesRcaGrepFirst(t *testing.T) {
	out := BuildSkillsAwarePrompt(nil)
	for _, needle := range []string{"rca_grep", "vm_run_cmd", "cmd.exe", "search_files", "禁止要求用户重述"} {
		if !strings.Contains(out, needle) {
			t.Fatalf("missing %q in skills prompt: %s", needle, out)
		}
	}
	if strings.Contains(out, "严格遵循") {
		t.Fatal("must not require strictly following a skill")
	}
}

func TestBuildSkillsSummary_hiddenAndPinned(t *testing.T) {
	all := []SkillMeta{
		{Name: "a", Description: "A"},
		{Name: "b", Description: "B"},
		{Name: "handbook-x", Description: "H", HiddenFromSummary: true},
		{Name: "code-map", Description: "M", SummaryPinned: true},
	}
	out := BuildSkillsSummary(all, 2)
	if strings.Contains(out, "handbook-x") {
		t.Fatalf("hidden skill listed: %s", out)
	}
	if !strings.Contains(out, "- code-map：M") || !strings.Contains(out, "- a：A") || strings.Contains(out, "- b：B") {
		t.Fatalf("pinned skill must come first within the cap: %s", out)
	}
	if all[0].Name != "a" || all[3].Name != "code-map" {
		t.Fatal("input slice must not be reordered")
	}
}

func TestBuildSkillsSummary_allHidden(t *testing.T) {
	if got := BuildSkillsSummary([]SkillMeta{{Name: "h", HiddenFromSummary: true}}, 8); got != "" {
		t.Fatalf("all hidden: got %q", got)
	}
}
