package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveInRepos_HappyAndTraversal(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	repoB := filepath.Join(base, "service-b")
	for _, d := range []string{repoA, repoB} {
		if err := os.MkdirAll(filepath.Join(d, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	roots := []string{repoA, repoB}

	full, root, err := resolveInRepos(roots, "service-b", "sub/x.go")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if root != repoB {
		t.Fatalf("root = %q, want %q", root, repoB)
	}
	wantFull := filepath.Join(repoB, "sub", "x.go")
	if full != wantFull {
		t.Fatalf("full = %q, want %q", full, wantFull)
	}

	if _, _, err := resolveInRepos(roots, "service-a", "../service-b/secret"); err == nil {
		t.Fatal("expected traversal to be rejected, got nil err")
	}

	if _, _, err := resolveInRepos(roots, "unknown", "x.go"); err == nil {
		t.Fatal("expected unknown repo error, got nil err")
	}

	if _, _, err := resolveInRepos(nil, "", "x.go"); err == nil {
		t.Fatal("expected empty roots error, got nil err")
	}

	// empty repo with valid roots must hit the "repo is required" guard
	if _, _, err := resolveInRepos(roots, "", "x.go"); err == nil {
		t.Fatal("expected 'repo is required' error for empty repo, got nil err")
	}
}

func TestRepoNameFromRoot(t *testing.T) {
	if got := repoNameFromRoot("/a/b/service-a"); got != "service-a" {
		t.Fatalf("repoNameFromRoot = %q, want service-a", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRCARegistry(t *testing.T, roots []string) *Registry {
	t.Helper()
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	if err := RegisterRCACodeTools(reg, roots); err != nil {
		t.Fatalf("RegisterRCACodeTools: %v", err)
	}
	return reg
}

func TestRCAGrep_MultiRepo(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	repoB := filepath.Join(base, "service-b")
	writeFile(t, filepath.Join(repoA, "a.go"), "package a\n// NullPointer here\n")
	writeFile(t, filepath.Join(repoB, "b.go"), "package b\nvar NullPointer = 1\n")
	reg := newRCARegistry(t, []string{repoA, repoB})

	tl, ok := reg.Get("rca_grep")
	if !ok {
		t.Fatal("rca_grep not registered")
	}
	out, err := tl.Execute(context.Background(), map[string]any{"pattern": "NullPointer"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	matches := m["matches"].([]map[string]any)
	if len(matches) != 2 {
		t.Fatalf("want 2 matches across repos, got %d: %v", len(matches), matches)
	}
	repos := map[string]bool{}
	for _, mm := range matches {
		repos[mm["repo"].(string)] = true
	}
	if !repos["service-a"] || !repos["service-b"] {
		t.Fatalf("expected matches from both repos, got %v", repos)
	}
	assertRCAEvidenceOK(t, m, "rca_grep")
	refs := m["evidence_refs"].([]EvidenceRef)
	if len(refs) != 2 {
		t.Fatalf("want 2 evidence_refs, got %#v", refs)
	}
}

func TestRCAGrep_EmptyRootsError(t *testing.T) {
	reg := newRCARegistry(t, nil)
	tl, _ := reg.Get("rca_grep")
	out, _ := tl.Execute(context.Background(), map[string]any{"pattern": "x"})
	m := out.(map[string]any)
	if _, ok := m["error"]; !ok {
		t.Fatal("expected error when roots empty")
	}
	assertRCAEvidenceError(t, m, ErrorPermanent)
}

func TestRCAGrep_MissingPatternPermanent(t *testing.T) {
	reg := newRCARegistry(t, []string{t.TempDir()})
	tl, _ := reg.Get("rca_grep")
	out, _ := tl.Execute(context.Background(), map[string]any{})
	assertRCAEvidenceError(t, out.(map[string]any), ErrorPermanent)
}

func TestRCAGrep_EmptyMatchesOK(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	writeFile(t, filepath.Join(repoA, "a.go"), "package a\n")
	reg := newRCARegistry(t, []string{repoA})
	tl, _ := reg.Get("rca_grep")
	out, err := tl.Execute(context.Background(), map[string]any{"pattern": "NoSuchTokenXYZ"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	assertRCAEvidenceOK(t, m, "rca_grep")
	refs := m["evidence_refs"].([]EvidenceRef)
	if refs[0].Summary != "no matches" {
		t.Fatalf("want empty-result summary, got %#v", refs[0])
	}
}

func TestRCAGrep_TruncationFlag(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	repoB := filepath.Join(base, "service-b")
	// repoA has exactly 2 matches; repoB has 0
	writeFile(t, filepath.Join(repoA, "a.go"), "NullPointer\nNullPointer\n")
	writeFile(t, filepath.Join(repoB, "b.go"), "package b\n")
	reg := newRCARegistry(t, []string{repoA, repoB})
	tl, _ := reg.Get("rca_grep")

	// exact quota, no more results anywhere -> truncated MUST be false
	out, err := tl.Execute(context.Background(), map[string]any{"pattern": "NullPointer", "max_results": 2})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	if len(m["matches"].([]map[string]any)) != 2 {
		t.Fatalf("want 2 matches, got %d", len(m["matches"].([]map[string]any)))
	}
	if m["truncated"].(bool) {
		t.Fatal("truncated should be false when exactly max_results found and nothing more remains")
	}

	// genuinely more than quota within one repo -> truncated MUST be true
	out2, _ := tl.Execute(context.Background(), map[string]any{"pattern": "NullPointer", "max_results": 1})
	m2 := out2.(map[string]any)
	if !m2["truncated"].(bool) {
		t.Fatal("truncated should be true when more matches exist than max_results")
	}
}

func TestRCAGlob_MultiRepo(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	repoB := filepath.Join(base, "service-b")
	writeFile(t, filepath.Join(repoA, "main.go"), "package a\n")
	writeFile(t, filepath.Join(repoA, "readme.md"), "x\n")
	writeFile(t, filepath.Join(repoB, "util.go"), "package b\n")
	reg := newRCARegistry(t, []string{repoA, repoB})

	tl, ok := reg.Get("rca_glob")
	if !ok {
		t.Fatal("rca_glob not registered")
	}
	out, err := tl.Execute(context.Background(), map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	matches := out.(map[string]any)["matches"].([]map[string]any)
	if len(matches) != 2 {
		t.Fatalf("want 2 .go files, got %d: %v", len(matches), matches)
	}
	for _, mm := range matches {
		if mm["repo"] == "" || mm["file"] == "" {
			t.Fatalf("match missing repo/file: %v", mm)
		}
	}
	assertRCAEvidenceOK(t, out.(map[string]any), "rca_glob")
}

func TestRCAGlob_RepoScoped(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	repoB := filepath.Join(base, "service-b")
	writeFile(t, filepath.Join(repoA, "main.go"), "package a\n")
	writeFile(t, filepath.Join(repoB, "util.go"), "package b\n")
	reg := newRCARegistry(t, []string{repoA, repoB})

	tl, _ := reg.Get("rca_glob")
	out, _ := tl.Execute(context.Background(), map[string]any{"pattern": "*.go", "repo": "service-a"})
	matches := out.(map[string]any)["matches"].([]map[string]any)
	if len(matches) != 1 || matches[0]["repo"].(string) != "service-a" {
		t.Fatalf("want only service-a match, got %v", matches)
	}
}

func TestRCAGlob_DoubleStarPathPattern(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "cloudgame")
	writeFile(t, filepath.Join(repo, "access-service", "go.mod"), "module access\n")
	writeFile(t, filepath.Join(repo, "api-gateway", "go.mod"), "module gateway\n")
	writeFile(t, filepath.Join(repo, "rock-stack", "apps", "cgsession", "go.mod"), "module session\n")
	writeFile(t, filepath.Join(repo, "readme.md"), "docs\n")
	reg := newRCARegistry(t, []string{repo})
	tl, ok := reg.Get("rca_glob")
	if !ok {
		t.Fatal("rca_glob not registered")
	}

	out, err := tl.Execute(context.Background(), map[string]any{
		"pattern":     "**/go.mod",
		"repo":        "cloudgame",
		"max_results": 50,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	matches, ok := m["matches"].([]map[string]any)
	if !ok {
		t.Fatalf("matches type: %T", m["matches"])
	}
	if len(matches) != 3 {
		t.Fatalf("want 3 go.mod matches for **/go.mod, got %d: %v", len(matches), matches)
	}

	// Nested path glob should still work.
	out2, err := tl.Execute(context.Background(), map[string]any{
		"pattern": "rock-stack/**/go.mod",
		"repo":    "cloudgame",
	})
	if err != nil {
		t.Fatalf("execute nested: %v", err)
	}
	matches2 := out2.(map[string]any)["matches"].([]map[string]any)
	if len(matches2) != 1 || matches2[0]["file"] != "rock-stack/apps/cgsession/go.mod" {
		t.Fatalf("want rock-stack nested go.mod, got %v", matches2)
	}
}

func TestRCAGlob_TruncationFlag(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	repoB := filepath.Join(base, "service-b")
	// repoA has exactly 2 .go; repoB has 0 .go
	writeFile(t, filepath.Join(repoA, "a.go"), "x\n")
	writeFile(t, filepath.Join(repoA, "b.go"), "x\n")
	writeFile(t, filepath.Join(repoB, "c.txt"), "x\n")
	reg := newRCARegistry(t, []string{repoA, repoB})
	tl, _ := reg.Get("rca_glob")

	// exact quota, nothing more anywhere -> truncated false
	out, _ := tl.Execute(context.Background(), map[string]any{"pattern": "*.go", "max_results": 2})
	m := out.(map[string]any)
	if len(m["matches"].([]map[string]any)) != 2 || m["truncated"].(bool) {
		t.Fatalf("want 2 matches truncated=false, got %v", m)
	}
	// more than quota -> truncated true
	out2, _ := tl.Execute(context.Background(), map[string]any{"pattern": "*.go", "max_results": 1})
	if !out2.(map[string]any)["truncated"].(bool) {
		t.Fatal("truncated should be true when more matches than max_results")
	}
}

func TestRCARead_HappyAndGuard(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	writeFile(t, filepath.Join(repoA, "svc", "handler.go"), "line1\nline2\nline3\n")
	reg := newRCARegistry(t, []string{repoA})

	tl, ok := reg.Get("rca_read")
	if !ok {
		t.Fatal("rca_read not registered")
	}
	out, err := tl.Execute(context.Background(), map[string]any{
		"repo": "service-a", "file": "svc/handler.go",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	content := m["content"].(string)
	if !strings.Contains(content, "1|line1") || !strings.Contains(content, "3|line3") {
		t.Fatalf("content missing numbered lines: %q", content)
	}
	if m["repo"].(string) != "service-a" {
		t.Fatalf("repo = %v", m["repo"])
	}
	assertRCAEvidenceOK(t, m, "rca_read")
	refs := m["evidence_refs"].([]EvidenceRef)
	if refs[0].Repo != "service-a" || refs[0].Path != "svc/handler.go" {
		t.Fatalf("refs=%#v", refs)
	}

	out2, _ := tl.Execute(context.Background(), map[string]any{
		"repo": "service-a", "file": "../secret.txt",
	})
	assertRCAEvidenceError(t, out2.(map[string]any), ErrorPermanent)

	out3, _ := tl.Execute(context.Background(), map[string]any{"file": "svc/handler.go"})
	assertRCAEvidenceError(t, out3.(map[string]any), ErrorPermanent)
}

func TestRCARead_TrailingNewlineLineCount(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	writeFile(t, filepath.Join(repoA, "f.go"), "a\nb\nc\n")
	reg := newRCARegistry(t, []string{repoA})
	tl, _ := reg.Get("rca_read")
	out, _ := tl.Execute(context.Background(), map[string]any{"repo": "service-a", "file": "f.go"})
	m := out.(map[string]any)
	if m["total_lines"].(int) != 3 {
		t.Fatalf("total_lines = %v, want 3", m["total_lines"])
	}
	if strings.Contains(m["content"].(string), "4|") {
		t.Fatalf("should not emit a 4th empty line: %q", m["content"])
	}
}

func TestRCAToolsetDefaults(t *testing.T) {
	for _, name := range []string{"rca_grep", "rca_glob", "rca_read", "jaeger_trace", "es_log_query"} {
		if got := builtinDefaultToolset[name]; got != ToolsetRCA {
			t.Fatalf("toolset[%s] = %q, want %q", name, got, ToolsetRCA)
		}
	}
}

func TestRCAGrepDescriptionPrefersCodeRoots(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	if err := os.MkdirAll(repoA, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := newRCARegistry(t, []string{repoA})
	tl, ok := reg.Get("rca_grep")
	if !ok {
		t.Fatal("rca_grep missing")
	}
	if !strings.Contains(tl.Description, "code roots") {
		t.Fatalf("description should mention code roots, got %q", tl.Description)
	}
	if strings.Contains(tl.Description, "inside RCA roots") {
		t.Fatalf("description should not treat RCA as the only use, got %q", tl.Description)
	}
	read, ok := reg.Get("rca_read")
	if !ok {
		t.Fatal("rca_read missing")
	}
	if !strings.Contains(read.Description, "verbatim") || !strings.Contains(read.Description, "enclosing function") {
		t.Fatalf("rca_read should require whole-function verbatim quotes, got %q", read.Description)
	}
}
