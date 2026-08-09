package lsp

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathURIRoundTrip(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	uri := PathToURI(file)
	got, err := URIToPath(uri)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got) != filepath.Clean(file) {
		t.Fatalf("got %q want %q", got, file)
	}
}

func TestURIToPath_RejectsNonFile(t *testing.T) {
	if _, err := URIToPath("https://example.com/x"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPathToURI_WindowsDrive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	uri := PathToURI(`D:\workspace\foo.go`)
	if uri == "" || uri[:8] != "file:///" {
		t.Fatalf("unexpected uri %q", uri)
	}
}
