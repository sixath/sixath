package chat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnderRoot_OK(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	abs, err := ResolveUnderRoot(root, "a/b")
	if err != nil || abs != sub {
		t.Fatalf("abs=%q err=%v", abs, err)
	}
}

func TestUnderRoot_RejectDotDot(t *testing.T) {
	root := t.TempDir()
	if _, err := ResolveUnderRoot(root, "../x"); err == nil {
		t.Fatal("expected error")
	}
}

func TestUnderRoot_RejectAbsPath(t *testing.T) {
	root := t.TempDir()
	if _, err := ResolveUnderRoot(root, root); err == nil {
		t.Fatal("expected error")
	}
}

func TestListDirs_OnlyDirectories(t *testing.T) {
	root := t.TempDir()
	_ = os.Mkdir(filepath.Join(root, "d1"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "f1"), []byte("x"), 0o644)
	ents, err := ListCodeDirs(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name != "d1" {
		t.Fatalf("%+v", ents)
	}
}

func TestWorkspaceUnderAnyRoot(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "repo")
	_ = os.Mkdir(ws, 0o755)
	if !WorkspaceUnderCodeRoots(ws, []string{root}) {
		t.Fatal("expected true")
	}
	if WorkspaceUnderCodeRoots(t.TempDir(), []string{root}) {
		t.Fatal("expected false")
	}
}

func TestUnderRoot_RejectSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	escape := filepath.Join(root, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Skip("symlink not permitted:", err)
	}
	if _, err := ResolveUnderRoot(root, "escape"); err == nil {
		t.Fatal("expected symlink escape to fail")
	}
}
