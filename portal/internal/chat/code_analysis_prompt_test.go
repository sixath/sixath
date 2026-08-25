package chat

import (
	"strings"
	"testing"
)

func TestAppendCodeAnalysisPrompt_Generic(t *testing.T) {
	got := AppendCodeAnalysisPrompt("")
	for _, want := range []string{"rca_grep", "code roots", "入边", "control_flow", "整个函数", "填结构体字段", "伪源码", "rca_symbol", "references", "inbound_empty", "1105", "call_graph", "MEMORY.md", "repos_scanned", "list_tables", "skill_view", "怎么写给用户", "不要整表贴进终答"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in prompt:\n%s", want, got)
		}
	}
	for _, ban := range []string{"存档迁移", "咪咕", "union-archiver", "migu"} {
		if strings.Contains(got, ban) {
			t.Fatalf("prompt must stay generic, found %q:\n%s", ban, got)
		}
	}
}

func TestAppendCodeAnalysisPromptIf_OnlyWhenCodeActive(t *testing.T) {
	base := "sys"
	if got := AppendCodeAnalysisPromptIf(familySet([]string{FamilyCore, "mcp:gitlab"}), base); got != base {
		t.Fatalf("gitlab-only must not append, got %q", got)
	}
	if got := AppendCodeAnalysisPromptIf(familySet([]string{FamilyCore, FamilyCode}), base); !strings.Contains(got, "rca_grep") {
		t.Fatalf("code family must append, got %q", got)
	}
	if got := AppendCodeAnalysisPromptIf(nil, base); !strings.Contains(got, "rca_grep") {
		t.Fatalf("nil active (surface off) must append, got %q", got)
	}
}
