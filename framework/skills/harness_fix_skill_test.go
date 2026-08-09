package skills

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestHarnessFixSkill_IndexedFromExamples(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	examplesSkills := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "skills_examples", "skills"))
	idx, err := NewIndex([]string{examplesSkills}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	meta, ok := idx.GetByName("harness-fix")
	if !ok {
		names := make([]string, 0)
		for _, m := range idx.All() {
			names = append(names, m.Name)
		}
		t.Fatalf("harness-fix not found; have %v", names)
	}
	if meta.Description == "" {
		t.Fatal("empty description")
	}
	found := false
	for _, toolName := range meta.AllowedTools {
		if toolName == "skill_manage" {
			found = true
		}
	}
	if !found {
		t.Fatalf("allowed_tools should include skill_manage, got %v", meta.AllowedTools)
	}
}
