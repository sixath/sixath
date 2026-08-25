package tool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildCallGraph_sameFileResolved(t *testing.T) {
	src := []byte(`package p
func Outer() {
	Inner()
}
func Inner() {}
`)
	cf := ExtractControlFlow(src, "a.go", 1, 20)
	cg := BuildCallGraph(src, "", "a.go", cf)
	if cg == nil {
		t.Fatal("call graph nil")
	}
	resolved := false
	for _, n := range cg.Nodes {
		if n.Name == "Inner" && n.Resolved && n.File == "a.go" {
			resolved = true
		}
	}
	if !resolved {
		t.Fatalf("Inner should be resolved in-file: %#v", cg.Nodes)
	}
}

func TestBuildCallGraph_nonGoNil(t *testing.T) {
	if cg := BuildCallGraph([]byte("x"), "a.py", "a.py", nil); cg != nil {
		t.Fatalf("%#v", cg)
	}
}

func TestBuildCallGraph_siblingDir(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "helper.go")
	db := filepath.Join(dir, "db.go")
	src := []byte("package p\nfunc Outer() { Inner() }\n")
	if err := os.WriteFile(helper, src, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db, []byte("package p\nfunc Inner() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cf := ExtractControlFlow(src, "helper.go", 1, 20)
	cg := BuildCallGraph(src, helper, "helper.go", cf)
	if cg == nil {
		t.Fatal("nil graph")
	}
	found := false
	for _, n := range cg.Nodes {
		if n.Name == "Inner" && n.Resolved && n.File == "db.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("nodes=%#v", cg.Nodes)
	}
}
