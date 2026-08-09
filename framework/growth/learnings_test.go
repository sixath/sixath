package growth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadWorkspaceLearnings_walksUpToRepoRoot(t *testing.T) {
	root := t.TempDir()
	learnDir := filepath.Join(root, ".learnings")
	if err := os.MkdirAll(learnDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(learnDir, "LEARNINGS.md"), []byte("# L\nfix ssh locally"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(root, "data", "workspaces", "agent-a")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	out := ReadWorkspaceLearnings(ws, 8000)
	if !strings.Contains(out, "fix ssh locally") {
		t.Fatalf("expected learnings content, got: %q", out)
	}
}
