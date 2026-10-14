package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func writeNote(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanPromotedOnly(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "ops/a.md", "---\nstatus: promoted\ntype: experience\n---\n\nbodyA")
	writeNote(t, dir, "_agent_notes/b.md", "---\nstatus: pending\n---\n\nbodyB")
	writeNote(t, dir, "mine/c.md", "---\nstatus: promoted\n---\n\nbodyC")
	writeNote(t, dir, ".obsidian/workspace.json", "{}")
	writeNote(t, dir, "ops/not-promoted.md", "---\nstatus: pending\n---\n\nbodyN")

	notes, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("want 1 promoted note in ops/, got %d", len(notes))
	}
	if notes[0].Zone != zonePromoted {
		t.Fatalf("want promoted zone, got %v", notes[0].Zone)
	}
	if notes[0].Path != filepath.Join(dir, "ops", "a.md") {
		t.Fatalf("bad path: %s", notes[0].Path)
	}
}

func TestScanEmptyRoot(t *testing.T) {
	notes, err := Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if notes != nil {
		t.Fatalf("empty root should return nil, got %v", notes)
	}
}
