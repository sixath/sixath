package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/skills"
)

func TestBuildEffectiveSystemPromptForTurn_injectsMatchedSkill(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "demo-route")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillMD := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillMD, []byte("---\nname: demo-route\ndescription: demo routing skill\n---\n# Demo\nFollow step A."), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := skills.NewIndex([]string{filepath.Join(dir, "skills")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	SetSkillRouteSettings(SkillRouteSettings{Enabled: true, MinScore: 5, MaxBodyRunes: 4000})
	out := BuildEffectiveSystemPromptForTurn("", idx, "please use demo-route for this task")
	if !strings.Contains(out, "【已自动匹配 Skill: demo-route") {
		t.Fatalf("expected auto-route banner, got: %s", out)
	}
	if !strings.Contains(out, "Follow step A") {
		t.Fatalf("expected skill body injected: %s", out)
	}
}

func TestBuildEffectiveSystemPromptForTurn_disabled(t *testing.T) {
	SetSkillRouteSettings(SkillRouteSettings{Enabled: false})
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "x")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: x\ndescription: x\n---\nbody"), 0o644)
	idx, _ := skills.NewIndex([]string{filepath.Join(dir, "skills")}, nil, nil)
	out := BuildEffectiveSystemPromptForTurn("", idx, "use x skill")
	if strings.Contains(out, "已自动匹配 Skill") {
		t.Fatal("should not inject when disabled")
	}
}
