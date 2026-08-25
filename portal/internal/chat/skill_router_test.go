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
	if strings.Contains(out, "请优先遵循此工作流") {
		t.Fatalf("high tier must not prefer the workflow over the user question: %s", out)
	}
	bodyAt := strings.Index(out, "Follow step A")
	clauseAt := strings.Index(out, "不得替换")
	if clauseAt < 0 {
		clauseAt = strings.Index(out, "不可改写")
	}
	if clauseAt < 0 {
		t.Fatalf("want anti-rewrite clause after body, got: %s", out)
	}
	if bodyAt > clauseAt {
		t.Fatalf("clause must come after skill body, bodyAt=%d clauseAt=%d", bodyAt, clauseAt)
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
	if strings.Contains(out, "【已自动匹配 Skill:") {
		t.Fatal("should not inject when disabled")
	}
}

func TestBuildEffectiveSystemPromptForTurnOnSurface_SkipsSkillsWhenCodeOnly(t *testing.T) {
	t.Setenv(toolFamilySplitEnv, "1")
	SetSkillRouteSettings(SkillRouteSettings{Enabled: true, MinScore: 1, MaxBodyRunes: 4000})
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "demo-route")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo-route\ndescription: demo\n---\n# Demo\nbody"), 0o644)
	idx, err := skills.NewIndex([]string{filepath.Join(dir, "skills")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := BuildEffectiveSystemPromptForTurnOnSurface("sys", idx, "please use demo-route", "", "", familySet([]string{FamilyCore, FamilyCode}))
	if strings.Contains(out, "已自动匹配") || strings.Contains(out, "可用 Skills") {
		t.Fatalf("code surface must not inject skills, got %s", out)
	}
	if out != "sys" {
		t.Fatalf("want original prompt, got %q", out)
	}
}

func TestBuildEffectiveSystemPromptForTurn_midTierNoBody(t *testing.T) {
	dir := t.TempDir()
	writeSkill := func(name, desc string) {
		d := filepath.Join(dir, "skills", name)
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(
			"---\nname: "+name+"\ndescription: "+desc+"\ntags: [kubernetes]\n---\n# BodyOf"+name+"\nDo not inject this heading in mid tier.\n",
		), 0o644)
	}
	writeSkill("alpha-helper", "cluster ops")
	writeSkill("beta-helper", "cluster ops")
	idx, err := skills.NewIndex([]string{filepath.Join(dir, "skills")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	SetSkillRouteSettings(SkillRouteSettings{Enabled: true, MinScore: 5, MaxBodyRunes: 4000})
	out := BuildEffectiveSystemPromptForTurn("", idx, "debug kubernetes pod crash")
	if strings.Contains(out, "BodyOf") || strings.Contains(out, "Do not inject this heading") {
		t.Fatalf("mid tier must not inject SKILL body, got %s", out)
	}
	if !strings.Contains(out, "【候选 Skill:") {
		t.Fatalf("want one-line candidate, got %s", out)
	}
}

func TestBuildEffectiveSystemPromptForTurn_bf26DoesNotInjectArchiveBody(t *testing.T) {
	dir := t.TempDir()
	write := func(name, fm, body string) {
		d := filepath.Join(dir, "skills", name)
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\nname: "+name+"\n"+fm+"\n---\n"+body+"\n"), 0o644)
	}
	write("rca-sync-archive-migrate", "description: 实时存档迁移 SyncDispatch\ntags: [es, rca]", "# Archive\n### 第 0 步：收集关键信息\n")
	write("migu-cloud-game-vm-allocate", "description: call chain across access-service union-access vm-manager", "# VM\nAssignVM path\n")
	idx, err := skills.NewIndex([]string{filepath.Join(dir, "skills")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	SetSkillRouteSettings(SkillRouteSettings{Enabled: true, MinScore: 5, MaxBodyRunes: 4000})
	q := "需要看看access-service有没有收到游戏启动成功事件的时间和vm-manager有没有startGame成功"
	out := BuildEffectiveSystemPromptForTurn("", idx, q)
	if strings.Contains(out, "第 0 步") || strings.Contains(out, "请优先遵循") {
		t.Fatal(out)
	}
	if strings.Contains(out, "AssignVM") || strings.Contains(out, "【已自动匹配 Skill:") {
		t.Fatalf("P1: sole match must not inject full SKILL body, got %s", out)
	}
}

func TestBuildEffectiveSystemPromptForTurn_soleMatchWithoutNameIsMid(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, "skills", "solo-helper")
	_ = os.MkdirAll(d, 0o755)
	_ = os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(
		"---\nname: solo-helper\ndescription: cluster ops\ntags: [kubernetes]\n---\n# BodyOfSolo\nDo not inject sole-match body.\n",
	), 0o644)
	idx, err := skills.NewIndex([]string{filepath.Join(dir, "skills")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	SetSkillRouteSettings(SkillRouteSettings{Enabled: true, MinScore: 5, MaxBodyRunes: 4000})
	out := BuildEffectiveSystemPromptForTurn("", idx, "debug kubernetes pod crash")
	if strings.Contains(out, "BodyOfSolo") || strings.Contains(out, "Do not inject sole-match body") {
		t.Fatalf("sole match without name in Q must be mid-tier, got %s", out)
	}
	if !strings.Contains(out, "【候选 Skill: solo-helper】") {
		t.Fatalf("want mid-tier candidate line, got %s", out)
	}
}
