package growth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendErrorLearning_writesERRORS(t *testing.T) {
	root := t.TempDir()
	if err := AppendErrorLearning(root, "tool boom", "details here", "ops"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".learnings", "ERRORS.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "**Status**: pending") {
		t.Fatalf("missing pending status: %s", s)
	}
	if !strings.Contains(s, "tool boom") {
		t.Fatalf("missing summary: %s", s)
	}
	if !strings.Contains(s, "details here") {
		t.Fatalf("missing details: %s", s)
	}
	if !strings.Contains(s, "**Area**: ops") {
		t.Fatalf("missing area: %s", s)
	}
}
