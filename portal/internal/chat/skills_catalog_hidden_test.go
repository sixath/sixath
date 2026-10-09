package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
)

func TestSkillsCatalogProvider_SkipsHidden(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "handbook-x", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("---\nname: handbook-x\ndescription: secret map\nhidden_from_summary: true\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := (&SkillsCatalogProvider{Index: idx}).Enrich(context.Background(), []tool.ToolCatalogEntry{{Name: "load_skill"}})
	if strings.Contains(strings.Join(out[0].SearchHints, " "), "handbook-x") {
		t.Fatalf("hidden skill leaked into hints: %#v", out[0].SearchHints)
	}
}
