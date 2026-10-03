package toolskill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sixath/framework/skills"
	core "github.com/sixath/framework/tool"
)

func newNameCheckIndex(t *testing.T) *skills.Index {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"rca-flow", "vm-pool"} {
		d := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Join(d, "docs"), 0o755); err != nil {
			t.Fatal(err)
		}
		md := "---\nname: " + name + "\ndescription: test\n---\n# " + name
		if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(md), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "docs", "a.md"), []byte("a"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

func assertOneOf(t *testing.T, label string, err error, want string) {
	t.Helper()
	var iae *core.InvalidArgumentsError
	if !errors.As(err, &iae) || iae.Errors[0].Keyword != core.KeywordOneOf {
		t.Fatalf("%s: got %v", label, err)
	}
	if c := iae.Errors[0].Candidates; len(c) == 0 || c[0] != want {
		t.Fatalf("%s: candidates %v", label, c)
	}
}

func TestSkillTools_UnknownNameSuggests(t *testing.T) {
	idx := newNameCheckIndex(t)
	reg := core.NewRegistry()
	if err := RegisterLoadSkillTool(reg, idx, nil); err != nil {
		t.Fatal(err)
	}
	if err := RegisterSkillsListViewTools(reg, idx, nil); err != nil {
		t.Fatal(err)
	}
	cases := map[string]map[string]any{
		"load_skill":      {"name": "rca-flw"},
		"skill_view":      {"name": "rca-flw"},
		"read_skill_file": {"name": "rca-flw", "path": "docs/a.md"},
	}
	for name, params := range cases {
		tl, _ := reg.Get(name)
		_, err := tl.Execute(context.Background(), params)
		assertOneOf(t, name, err, "rca-flow")
	}
	tl, _ := reg.Get("load_skill")
	if _, err := tl.Execute(context.Background(), map[string]any{"name": "rca-flow"}); err != nil {
		t.Fatalf("known name: %v", err)
	}
}

func TestSkillNameCheck_CaseInsensitive(t *testing.T) {
	idx := newNameCheckIndex(t)
	c := skillNameCheck(staticSkillIndex(idx), nil, nil)
	if errs, err := c.Check(context.Background(), map[string]any{"name": "RCA-Flow"}); err != nil || len(errs) != 0 {
		t.Fatalf("case-only difference must pass: errs=%v err=%v", errs, err)
	}
	if errs, _ := c.Check(context.Background(), map[string]any{"name": "rca-flw"}); len(errs) != 1 {
		t.Fatalf("typo must still be rejected: %v", errs)
	}
}

func TestSkillManage_NameCheckSkipsCreate(t *testing.T) {
	root := t.TempDir()
	cfg := skillManageTestConfig(nil, false)
	cfg.Index = newNameCheckIndex(t)
	tl := registerSkillManageForTest(t, cfg)
	ctx := skillManageTestCtx(root)

	res, err := tl.Execute(ctx, map[string]any{
		"action": "create", "name": "brand-new",
		"content": "---\nname: brand-new\ndescription: test\n---\n# x",
	})
	if err != nil {
		t.Fatalf("create must not be name-checked: %v", err)
	}
	if m := res.(map[string]any); m["status"] != "ok" {
		t.Fatalf("create: %#v", m)
	}

	_, err = tl.Execute(ctx, map[string]any{"action": "edit", "name": "vm-pol", "content": "x"})
	assertOneOf(t, "edit", err, "vm-pool")

	_, err = tl.Execute(ctx, map[string]any{
		"action": "edit", "name": "brand-new",
		"content": "---\nname: brand-new\ndescription: test2\n---\n# y",
	})
	if err != nil {
		t.Fatalf("skill created in workspace this session must pass the check: %v", err)
	}
}
