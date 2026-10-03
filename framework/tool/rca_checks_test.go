package tool

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newRCACheckRepo(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	repoA := filepath.Join(base, "repoA")
	writeFile(t, filepath.Join(repoA, "main.go"), "package main\n")
	writeFile(t, filepath.Join(repoA, "internal", "handler.go"), "package internal\n")
	return base, repoA
}

func TestRCAGrep_UnknownRepoSuggests(t *testing.T) {
	_, repoA := newRCACheckRepo(t)
	reg := newRCARegistry(t, []string{repoA})
	for _, name := range []string{"rca_grep", "rca_glob", "rca_read"} {
		tl, _ := reg.Get(name)
		params := map[string]any{"pattern": "x", "repo": "repoa", "file": "main.go"}
		out, err := tl.Execute(context.Background(), params)
		var iae *InvalidArgumentsError
		if !errors.As(err, &iae) || iae.Errors[0].Keyword != KeywordOneOf {
			t.Fatalf("%s: got %v", name, err)
		}
		if c := iae.Errors[0].Candidates; len(c) == 0 || c[0] != "repoA" {
			t.Fatalf("%s: candidates %v", name, c)
		}
		assertRCAEvidenceError(t, out.(map[string]any), ErrorPermanent)
	}
}

func TestRCARead_MissingFileSuggestsSimilar(t *testing.T) {
	_, repoA := newRCACheckRepo(t)
	reg := newRCARegistry(t, []string{repoA})
	tl, _ := reg.Get("rca_read")
	out, err := tl.Execute(context.Background(), map[string]any{"repo": "repoA", "file": "internal/handlr.go"})
	if err != nil {
		t.Fatal(err)
	}
	sim, _ := out.(map[string]any)["similar"].([]string)
	if len(sim) == 0 || sim[0] != "internal/handler.go" {
		t.Fatalf("similar=%v full=%v", sim, out)
	}

	out, _ = tl.Execute(context.Background(), map[string]any{"repo": "repoA", "file": "other/handlr.go"})
	sim, _ = out.(map[string]any)["similar"].([]string)
	if len(sim) == 0 || sim[0] != "internal/handler.go" {
		t.Fatalf("repo-wide fallback similar=%v", sim)
	}
}

func TestRCAGlob_HitStatusAndMissingRoots(t *testing.T) {
	base, repoA := newRCACheckRepo(t)
	missing := filepath.Join(base, "gone")

	reg := newRCARegistry(t, []string{missing})
	tl, _ := reg.Get("rca_glob")
	out, err := tl.Execute(context.Background(), map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["hit_status"] != HitStatusSuspect {
		t.Fatalf("missing root: %v", m)
	}
	if rm, _ := m["roots_missing"].([]string); len(rm) != 1 || rm[0] != "gone" {
		t.Fatalf("roots_missing=%v", m["roots_missing"])
	}

	reg = newRCARegistry(t, []string{repoA, missing})
	tl, _ = reg.Get("rca_glob")
	out, _ = tl.Execute(context.Background(), map[string]any{"pattern": "*.py"})
	m = out.(map[string]any)
	if m["hit_status"] != HitStatusSuspect || m["roots_missing"] == nil {
		t.Fatalf("partial missing with 0 hits must be suspect: %v", m)
	}
	out, _ = tl.Execute(context.Background(), map[string]any{"pattern": "*.go"})
	m = out.(map[string]any)
	if m["hit_status"] != HitStatusHits || m["roots_missing"] != nil {
		t.Fatalf("hits: %v", m)
	}

	reg = newRCARegistry(t, []string{repoA})
	tl, _ = reg.Get("rca_glob")
	out, _ = tl.Execute(context.Background(), map[string]any{"pattern": "*.py"})
	if m = out.(map[string]any); m["hit_status"] != HitStatusEmpty {
		t.Fatalf("empty: %v", m)
	}
}

func TestSearchFiles_RootMissingIsSuspect(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "a.go"), "package a\nfunc Foo() {}\n")
	reg := registerFileToolsForTest(t)
	tl, _ := reg.Get("search_files")
	for _, target := range []string{"content", "files"} {
		out, err := tl.Execute(fileToolsCtx(root), map[string]any{"pattern": "Foo", "target": target, "path": "nope"})
		if err != nil {
			t.Fatal(err)
		}
		m := out.(map[string]any)
		if m["hit_status"] != HitStatusSuspect || m["root_missing"] != true || m["error"] != nil {
			t.Fatalf("%s missing root: %v", target, m)
		}

		out, _ = tl.Execute(fileToolsCtx(root), map[string]any{"pattern": "NoSuchThing*", "target": target, "path": "src"})
		m = out.(map[string]any)
		if m["hit_status"] != HitStatusEmpty || m["root_missing"] != nil {
			t.Fatalf("%s empty: %v", target, m)
		}
	}
	out, _ := tl.Execute(fileToolsCtx(root), map[string]any{"pattern": "*.go", "target": "files"})
	if m := out.(map[string]any); m["hit_status"] != HitStatusHits || m["root"] != "." {
		t.Fatalf("hits: %v", m)
	}
}
