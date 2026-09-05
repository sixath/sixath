package toolskill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePatchBatch_rejectsPathOutsideWorkspace(t *testing.T) {
	ws := t.TempDir()
	root, err := filepath.Abs(ws)
	if err != nil {
		t.Fatal(err)
	}

	cases := []string{
		"../outside",
		"../etc/passwd",
		filepath.Join("..", "outside"),
	}
	for _, p := range cases {
		err := ValidatePatchBatch(root, []Patch{{Path: p, Op: OpCreate, Content: "x"}})
		if err == nil {
			t.Fatalf("expected error for path %q", p)
		}
	}
}

func TestValidatePatchBatch_acceptsPathUnderWorkspace(t *testing.T) {
	ws := t.TempDir()
	root, err := filepath.Abs(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}

	err = ValidatePatchBatch(root, []Patch{{Path: "skills/foo/SKILL.md", Op: OpDelete}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePatchBatch_emptyBatchIsNil(t *testing.T) {
	ws := t.TempDir()
	root, err := filepath.Abs(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePatchBatch(root, nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if err := ValidatePatchBatch(root, []Patch{}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidatePatchBatch_invalidOp(t *testing.T) {
	ws := t.TempDir()
	root, err := filepath.Abs(ws)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidatePatchBatch(root, []Patch{{Path: "a", Op: "nope"}})
	if err == nil {
		t.Fatal("expected error for invalid op")
	}
}

func TestValidatePatchBatch_patchRequiresOld(t *testing.T) {
	ws := t.TempDir()
	root, err := filepath.Abs(ws)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidatePatchBatch(root, []Patch{{Path: "f.txt", Op: OpPatch, New: "z"}})
	if err == nil {
		t.Fatal("expected error when Old is empty for patch")
	}
}
