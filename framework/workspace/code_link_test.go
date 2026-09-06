package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkCode_EmptyWorkspace(t *testing.T) {
	_, _, err := LinkCode("", t.TempDir(), []string{t.TempDir()})
	if !errors.Is(err, ErrEmptyWorkspace) {
		t.Fatalf("err=%v", err)
	}
}

func TestLinkCode_EscapeRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	ws := t.TempDir()
	_, _, err := LinkCode(ws, outside, []string{root})
	if !errors.Is(err, ErrTargetNotAllowed) {
		t.Fatalf("err=%v want ErrTargetNotAllowed", err)
	}
}

func TestLinkCode_SuccessAndIdempotent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "repo")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(t.TempDir(), "agent-ws")
	link, abs, err := LinkCode(ws, target, []string{root})
	if err != nil {
		if strings.Contains(err.Error(), "symlink") || strings.Contains(strings.ToLower(err.Error()), "privilege") {
			t.Skip("symlink not permitted:", err)
		}
		t.Fatal(err)
	}
	want := filepath.Join(ws, CodeDir)
	if link != want {
		t.Fatalf("link=%q want=%q", link, want)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected symlink")
	}
	if _, _, err := LinkCode(ws, target, []string{root}); err != nil {
		t.Fatalf("idempotent: %v", err)
	}
	if ResolveCodeMount(ws) == "" {
		t.Fatalf("ResolveCodeMount empty, abs=%q", abs)
	}
}

func TestLinkCode_Conflict(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	if err := os.Mkdir(a, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(b, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	if _, _, err := LinkCode(ws, a, []string{root}); err != nil {
		if strings.Contains(err.Error(), "symlink") || strings.Contains(strings.ToLower(err.Error()), "privilege") {
			t.Skip("symlink not permitted:", err)
		}
		t.Fatal(err)
	}
	_, _, err := LinkCode(ws, b, []string{root})
	if !errors.Is(err, ErrLinkConflict) {
		t.Fatalf("err=%v want ErrLinkConflict", err)
	}
}

func TestResolveCodeMount_Missing(t *testing.T) {
	if got := ResolveCodeMount(t.TempDir()); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := ResolveCodeMount(""); got != "" {
		t.Fatalf("empty root got %q", got)
	}
}
