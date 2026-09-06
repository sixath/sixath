package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCLIRoot_EmptyCreatesDotSath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	got, err := EnsureCLIRoot("")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(filepath.Join(dir, ".sath", "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	st, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsDir() {
		t.Fatalf("%q is not a directory", got)
	}
}

func TestEnsureCLIRoot_BlankSameAsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	got, err := EnsureCLIRoot("   ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(filepath.Join(dir, ".sath", "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEnsureCLIRoot_NonEmptyKeepsPath(t *testing.T) {
	dir := t.TempDir()
	got, err := EnsureCLIRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
