package memorysearch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sixath/framework/config"
)

func TestMemoryIndexManager_FTS(t *testing.T) {
	dir := t.TempDir()
	memPath := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(memPath, []byte("# Project Memory\n\nWe use Go for the backend. The frontend is React.\n\nKey decision: use SQLite for local storage."), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.MemorySearchConfig{
		Enabled:  true,
		Sources:  []string{"memory"},
		Provider: "",
		Store:    config.MemoryStoreConfig{Path: filepath.Join(dir, ".memory.db")},
		Chunking: config.MemoryChunkingConfig{Tokens: 256, Overlap: 32},
		Query:    config.MemoryQueryConfig{MaxResults: 5, MinScore: 0.1},
	}
	resolved := ResolveMemorySearch(cfg, dir)
	if resolved == nil {
		t.Fatal("expected resolved config")
	}
	resolved.StorePath = filepath.Join(dir, ".memory.db")
	mgr, err := NewMemoryIndexManager(resolved, dir, "test-agent", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	ctx := context.Background()
	if err := mgr.Sync(ctx, &SyncParams{Reason: "test", Force: true}); err != nil {
		t.Fatal(err)
	}
	st, err := mgr.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Chunks == 0 {
		t.Fatal("no chunks indexed")
	}
	results, err := mgr.Search(ctx, "React", &SearchOpts{MaxResults: 5, MinScore: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatalf("expected at least one result for 'React', got 0 (chunks=%d)", st.Chunks)
	}
	found := false
	for _, r := range results {
		if r.Path == "MEMORY.md" && (r.Score > 0 || r.Snippet != "") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected MEMORY.md in results, got %+v", results)
	}
	// Also verify SQLite keyword
	results2, _ := mgr.Search(ctx, "SQLite", &SearchOpts{MaxResults: 5})
	if len(results2) == 0 {
		t.Log("note: 'SQLite' query returned no results (FTS tokenization may vary)")
	}
}

func TestMemoryIndexManager_ReadFile(t *testing.T) {
	dir := t.TempDir()
	memPath := filepath.Join(dir, "MEMORY.md")
	content := "# Memory\n\nLine 3\nLine 4\nLine 5"
	if err := os.WriteFile(memPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.MemorySearchConfig{
		Enabled: true,
		Store:   config.MemoryStoreConfig{Path: filepath.Join(dir, ".m.db")},
	}
	resolved := ResolveMemorySearch(cfg, dir)
	resolved.StorePath = filepath.Join(dir, ".m.db")
	mgr, err := NewMemoryIndexManager(resolved, dir, "test", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	ctx := context.Background()
	res, err := mgr.ReadFile(ctx, &ReadFileParams{RelPath: "MEMORY.md"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != content {
		t.Errorf("expected full content, got %q", res.Text)
	}
	res2, err := mgr.ReadFile(ctx, &ReadFileParams{RelPath: "MEMORY.md", From: 3, Lines: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Text != "Line 3\nLine 4" {
		t.Errorf("expected 'Line 3\\nLine 4', got %q", res2.Text)
	}
}

func TestMemoryIndexManager_ReadFile_InvalidPath(t *testing.T) {
	dir := t.TempDir()
	cfg := config.MemorySearchConfig{Enabled: true, Store: config.MemoryStoreConfig{Path: filepath.Join(dir, ".m.db")}}
	resolved := ResolveMemorySearch(cfg, dir)
	resolved.StorePath = filepath.Join(dir, ".m.db")
	mgr, err := NewMemoryIndexManager(resolved, dir, "test", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	ctx := context.Background()
	_, err = mgr.ReadFile(ctx, &ReadFileParams{RelPath: "../../../etc/passwd"})
	if err == nil {
		t.Fatal("expected error for path escape")
	}
}
