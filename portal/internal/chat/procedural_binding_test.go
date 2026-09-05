package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/skills"
)

func TestProceduralBindingHints_suggest(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "escalation")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: escalation\ndescription: escalate\n---\n# Esc\nCall human."), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := skills.NewIndex([]string{filepath.Join(dir, "skills")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	SetProceduralRepairConfig(&config.MemoryProceduralRepair{
		Enabled: true,
		Bindings: []config.MemoryProceduralBindingYAML{{
			TriggerQuery: "转人工",
			ActionKind:   "skill",
			SkillID:      "escalation",
			Mode:         "suggest",
		}},
	})
	t.Cleanup(func() { SetProceduralRepairConfig(nil) })

	out := appendProceduralBindingHints("", "用户要求转人工", idx, "", "")
	if !strings.Contains(out, "过程修复") || !strings.Contains(out, "escalation") {
		t.Fatalf("expected procedural suggest, got: %s", out)
	}
}

func TestProceduralCatalog_DisableRemovesSuggest(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "escalation")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: escalation\ndescription: e\n---\n# E\n"), 0o644)
	idx, err := skills.NewIndex([]string{filepath.Join(dir, "skills")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	SetProceduralRepairConfig(&config.MemoryProceduralRepair{
		Enabled: true,
		Bindings: []config.MemoryProceduralBindingYAML{{
			TriggerQuery: "转人工",
			ActionKind:   "skill",
			SkillID:      "escalation",
		}},
	})
	t.Cleanup(func() { SetProceduralRepairConfig(nil) })

	id := memory.EntryIDForBinding(memory.ProceduralBinding{
		TriggerQuery: "转人工",
		ActionKind:   memory.BindingActionSkill,
		SkillID:      "escalation",
	})
	if !DisableProceduralEntry(id) {
		t.Fatal("disable")
	}
	out := appendProceduralBindingHints("", "转人工", idx, "", "")
	if strings.Contains(out, "过程修复") {
		t.Fatalf("disabled binding still suggested: %s", out)
	}
}
