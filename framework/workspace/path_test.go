package workspace

import (
	"strings"
	"testing"
)

func TestResolveWorkspacePath_RejectsTraversal(t *testing.T) {
	root := t.TempDir()
	_, err := ResolveWorkspacePath(root, "../outside.txt")
	if err == nil {
		t.Fatal("expected error for path escape")
	}
}

func TestResolveWorkspacePath_AcceptsRelative(t *testing.T) {
	root := t.TempDir()
	p, err := ResolveWorkspacePath(root, "skills/foo/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, "skills") {
		t.Fatalf("unexpected path %q", p)
	}
}

func TestResolveWorkspacePath_EmptyRoot(t *testing.T) {
	_, err := ResolveWorkspacePath("", "a.txt")
	if err == nil {
		t.Fatal("expected error")
	}
}
