package growth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/skills"
)

func TestFormatSkillsIndexSnapshot_truncates(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "SKILL.md")
	longDesc := strings.Repeat("x", 400)
	body := "---\nname: alpha\ndescription: " + longDesc + "\n---\n"
	if err := os.WriteFile(skillPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := FormatSkillsIndexSnapshot(idx, 5, 20)
	if s == "" {
		t.Fatal("empty snapshot")
	}
	if len(s) < 20 {
		t.Fatalf("unexpectedly short: %q", s)
	}
}
