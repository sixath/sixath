package growth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCuratorRunner_skipsBelowMinSkills(t *testing.T) {
	var called bool
	r := &CuratorRunner{deps: CuratorDeps{
		ProposeCuratorPatches: func(ctx context.Context, job CuratorJob, summary string) ([]Patch, error) {
			called = true
			return nil, nil
		},
	}}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "skills", "only-one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills", "only-one", "SKILL.md"),
		[]byte("---\nname: only-one\ndescription: x\n---\n# one"), 0o644); err != nil {
		t.Fatal(err)
	}
	job := CuratorJob{WorkspaceRoot: dir, WorkspaceKey: dir}
	if err := r.Run(context.Background(), job, 2); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("proposer should not run when skill count < minSkills")
	}
}

func TestCuratorRunner_appliesPatches(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "skills", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "skills", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		p := filepath.Join(workspace, "skills", name, "SKILL.md")
		if err := os.WriteFile(p, []byte("---\nname: "+name+"\ndescription: dup\n---\n# "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := DefaultSkillsIndexTracker.Generation(workspace)
	r := &CuratorRunner{deps: CuratorDeps{
		ProposeCuratorPatches: func(ctx context.Context, job CuratorJob, summary string) ([]Patch, error) {
			return []Patch{{
				Path:    "skills/umbrella/SKILL.md",
				Op:      OpCreate,
				Content: "---\nname: umbrella\ndescription: merged\n---\n# umbrella",
			}}, nil
		},
	}}
	job := CuratorJob{WorkspaceRoot: workspace, WorkspaceKey: workspace}
	if err := r.Run(context.Background(), job, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "skills", "umbrella", "SKILL.md")); err != nil {
		t.Fatalf("umbrella skill not created: %v", err)
	}
	if DefaultSkillsIndexTracker.Generation(workspace) != before+1 {
		t.Fatalf("generation want %d got %d", before+1, DefaultSkillsIndexTracker.Generation(workspace))
	}
}
