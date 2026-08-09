package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sixath/framework/growth"
)

func TestPatchProposerFromFile_nilWhenEmpty(t *testing.T) {
	if patchProposerFromFile("") != nil {
		t.Fatal("expected nil proposer")
	}
}

func TestPatchProposerFromFile_readsJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "patches.json")
	if err := os.WriteFile(p, []byte(`[{"path":"a.txt","op":"create","content":"z"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	prop := patchProposerFromFile(p)
	if prop == nil {
		t.Fatal("expected proposer")
	}
	batch, err := prop(context.Background(), growth.ReviewJob{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 1 || batch[0].Path != "a.txt" || batch[0].Op != growth.OpCreate {
		t.Fatalf("%#v", batch)
	}
}

func TestPatchProposerFromFile_missingFile(t *testing.T) {
	prop := patchProposerFromFile(filepath.Join(t.TempDir(), "nope.json"))
	_, err := prop(context.Background(), growth.ReviewJob{}, "", "")
	if err == nil {
		t.Fatal("expected error")
	}
}
