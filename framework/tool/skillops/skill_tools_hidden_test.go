package toolskill

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sixath/framework/skills"
	core "github.com/sixath/framework/tool"
)

func TestSkillsList_SkipsHidden(t *testing.T) {
	dir := t.TempDir()
	write := func(name, extra string) {
		p := filepath.Join(dir, name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: "+name+"\ndescription: d\n"+extra+"---\nbody"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("visible", "")
	write("handbook-x", "hidden_from_summary: true\n")
	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := core.NewRegistry()
	if err := registerSkillsListTool(reg, idx); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("skills_list")
	res, err := tl.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	items := res.(map[string]any)["skills"].([]map[string]any)
	if len(items) != 1 || items[0]["name"] != "visible" {
		t.Fatalf("skills_list = %#v", items)
	}
}
