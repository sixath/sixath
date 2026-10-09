package handbook

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStore_PublishPruneRead(t *testing.T) {
	s := Store{Root: t.TempDir()}
	for v := 1; v <= 3; v++ {
		files := map[string][]byte{"skill/SKILL.md": []byte("v" + string(rune('0'+v))), "skill/references/index.md": []byte("idx")}
		if err := s.Publish("r1", v, files, 2); err != nil {
			t.Fatal(err)
		}
	}
	if cur, err := s.Current("r1"); err != nil || cur != 3 {
		t.Fatalf("current = %d err=%v", cur, err)
	}
	if _, err := os.Stat(s.VersionDir("r1", 1)); !os.IsNotExist(err) {
		t.Fatal("v1 should be pruned")
	}
	if _, err := os.Stat(s.VersionDir("r1", 2)); err != nil {
		t.Fatal("v2 should be kept")
	}
	pages, err := s.ListSkillFiles("r1", 3)
	if err != nil || strings.Join(pages, ",") != "SKILL.md,references/index.md" {
		t.Fatalf("pages = %v err=%v", pages, err)
	}
	b, err := s.ReadSkillFile("r1", 3, "SKILL.md")
	if err != nil || string(b) != "v3" {
		t.Fatalf("read = %q err=%v", b, err)
	}
	for _, bad := range []string{"", "../manifest.json", "..", "/etc/passwd", `..\x`, "references/../../x"} {
		if _, err := s.ReadSkillFile("r1", 3, bad); !errors.Is(err, ErrBadPath) {
			t.Fatalf("ReadSkillFile(%q) err = %v, want ErrBadPath", bad, err)
		}
	}
	if _, err := s.ReadSkillFile("r1", 3, "nope.md"); !os.IsNotExist(err) {
		t.Fatalf("missing page err = %v", err)
	}
}

func TestStore_CurrentMissing(t *testing.T) {
	if v, err := (Store{Root: t.TempDir()}).Current("nope"); err != nil || v != 0 {
		t.Fatalf("v=%d err=%v", v, err)
	}
}

func TestWriteFileAtomic(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "b.txt")
	if err := WriteFileAtomic(p, []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(p, []byte("2")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "2" {
		t.Fatalf("content = %q", b)
	}
}
