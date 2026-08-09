package local_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/local"
)

func TestDirWiki_SearchAndRead(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "guide.md"), "# Setup\nUse memory hub wiki for docs.\n")
	mustWrite(t, filepath.Join(dir, "other.txt"), "unrelated content")
	w, err := local.NewDirWiki(dir)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := w.Search(context.Background(), "memory hub", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "guide.md" || hits[0].Source != "wiki" {
		t.Fatalf("%#v", hits)
	}
	if !strings.Contains(strings.ToLower(hits[0].Content), "memory hub") {
		t.Fatalf("snippet=%q", hits[0].Content)
	}
	got, err := w.Read(context.Background(), "guide.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Content, "Setup") {
		t.Fatalf("%q", got.Content)
	}
}

func TestDirWiki_RejectsEscape(t *testing.T) {
	dir := t.TempDir()
	w, err := local.NewDirWiki(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Read(context.Background(), "../secret.md")
	if err == nil {
		t.Fatal("expected escape error")
	}
}

func TestDirCodeGraph_SearchSymbols(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "pkg", "hub.go"), "package pkg\n\nfunc ResolveAgentHub() {}\n\nfunc Other() {}\n")
	mustWrite(t, filepath.Join(dir, "pkg", "util.ts"), "export function formatCodeHit() {}\n")
	g, err := local.NewDirCodeGraph(dir)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := g.Search(context.Background(), "ResolveAgentHub", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Source != "codegraph" {
		t.Fatalf("%#v", hits)
	}
	if !strings.Contains(hits[0].Content, "ResolveAgentHub") {
		t.Fatalf("%q", hits[0].Content)
	}
	pathHits, err := g.Search(context.Background(), "util.ts", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(pathHits) != 1 {
		t.Fatalf("%#v", pathHits)
	}
}

func TestLocalKnowledge_WikiBackendWired(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "alpha beta gamma")
	w, err := local.NewDirWiki(dir)
	if err != nil {
		t.Fatal(err)
	}
	k := local.NewLocalKnowledge(local.KnowledgeBackends{Wiki: w})
	if !k.Capabilities().Has("wiki") {
		t.Fatal("expected wiki capability")
	}
	out, err := k.Call(context.Background(), hub.Identity{}, "knowledge_search", map[string]any{
		"query": "beta", "source": "wiki",
	})
	if err != nil {
		t.Fatal(err)
	}
	hits := out.([]local.KnowledgeHit)
	if len(hits) != 1 {
		t.Fatalf("%#v", hits)
	}
	readOut, err := k.Call(context.Background(), hub.Identity{}, "knowledge_read", map[string]any{
		"id": "a.md", "source": "wiki",
	})
	if err != nil {
		t.Fatal(err)
	}
	hit := readOut.(local.KnowledgeHit)
	if !strings.Contains(hit.Content, "alpha") {
		t.Fatalf("%#v", hit)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
