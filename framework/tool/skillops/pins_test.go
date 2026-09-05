package toolskill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPinnedSkillNames(t *testing.T) {
	root := t.TempDir()
	growthDir := filepath.Join(root, ".growth")
	if err := os.MkdirAll(growthDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(growthDir, "pinned_skills.json"), []byte(`{"pinned":["a-skill","b-skill"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	pins, err := LoadPinnedSkillNames(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pins["a-skill"]; !ok {
		t.Fatalf("missing a-skill: %#v", pins)
	}
	if _, ok := pins["b-skill"]; !ok {
		t.Fatalf("missing b-skill: %#v", pins)
	}
	ok, err := IsSkillPinned(root, "a-skill")
	if err != nil || !ok {
		t.Fatalf("IsSkillPinned a-skill: ok=%v err=%v", ok, err)
	}
}
