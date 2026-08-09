package growth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyPatchBatch_atomicRollbackOnMidFailure(t *testing.T) {
	ws := t.TempDir()
	root, err := filepath.Abs(ws)
	if err != nil {
		t.Fatal(err)
	}

	// First create succeeds; second patch fails because "l" is not unique in "hello".
	batch := []Patch{
		{Path: "x/a.txt", Op: OpCreate, Content: "hello"},
		{Path: "x/a.txt", Op: OpPatch, Old: "l", New: "L"},
	}
	if err := ApplyPatchBatch(root, batch); err == nil {
		t.Fatal("expected error from non-unique Old")
	}

	full := filepath.Join(root, "x", "a.txt")
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Fatalf("expected first create rolled back (file absent), stat err=%v", err)
	}
}

func TestApplyPatchBatch_singleCreate(t *testing.T) {
	ws := t.TempDir()
	root, err := filepath.Abs(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyPatchBatch(root, []Patch{{Path: "n.txt", Op: OpCreate, Content: "ok"}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "n.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "ok" {
		t.Fatalf("content %q", b)
	}
}

func TestApplyPatchBatch_createThenPatch(t *testing.T) {
	ws := t.TempDir()
	root, err := filepath.Abs(ws)
	if err != nil {
		t.Fatal(err)
	}
	batch := []Patch{
		{Path: "p.txt", Op: OpCreate, Content: "alpha"},
		{Path: "p.txt", Op: OpPatch, Old: "alpha", New: "beta"},
	}
	if err := ApplyPatchBatch(root, batch); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "p.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "beta" {
		t.Fatalf("got %q", b)
	}
}

func TestApplyPatchBatch_delete(t *testing.T) {
	ws := t.TempDir()
	root, err := filepath.Abs(ws)
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(root, "d.txt")
	if err := os.WriteFile(full, []byte("gone"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPatchBatch(root, []Patch{{Path: "d.txt", Op: OpDelete}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Fatalf("expected deleted, err=%v", err)
	}
}
