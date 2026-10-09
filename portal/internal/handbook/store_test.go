package handbook

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
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
	bads := []string{"", "../manifest.json", "..", "/etc/passwd", `..\x`, "references/../../x",
		".. ", "SKILL.md.", "references/ index.md", "C:x", "references/a b.md"}
	if runtime.GOOS == "windows" {
		bads = append(bads, "AUX", "CON.md", "references/NUL", "com1.txt")
	}
	for _, bad := range bads {
		if _, err := s.ReadSkillFile("r1", 3, bad); !errors.Is(err, ErrBadPath) {
			t.Fatalf("ReadSkillFile(%q) err = %v, want ErrBadPath", bad, err)
		}
	}
	if _, err := s.ReadSkillFile("r1", 3, "nope.md"); !os.IsNotExist(err) {
		t.Fatalf("missing page err = %v", err)
	}
	if b, err := s.ReadSkillFile("r1", 3, `references\index.md`); err != nil || string(b) != "idx" {
		t.Fatalf("backslash read = %q err=%v", b, err)
	}
	if _, err := s.ReadSkillFile("r1", 3, "references"); !errors.Is(err, ErrBadPath) {
		t.Fatalf("directory read err = %v, want ErrBadPath", err)
	}
}

func TestStore_PublishNeverClobbersAVersion(t *testing.T) {
	s := Store{Root: t.TempDir()}
	if err := s.Publish("r1", 1, map[string][]byte{"skill/SKILL.md": []byte("first")}, 2); err != nil {
		t.Fatal(err)
	}
	err := s.Publish("r1", 1, map[string][]byte{"skill/SKILL.md": []byte("second")}, 2)
	if !errors.Is(err, ErrVersionExists) {
		t.Fatalf("republish err = %v, want ErrVersionExists", err)
	}
	if b, err := s.ReadSkillFile("r1", 1, "SKILL.md"); err != nil || string(b) != "first" {
		t.Fatalf("published version changed: %q err=%v", b, err)
	}
	ents, err := os.ReadDir(filepath.Join(s.Root, "repos", "r1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("staging dir left behind: %s", e.Name())
		}
	}
}

func TestStore_ConcurrentPublishSameVersion(t *testing.T) {
	s := Store{Root: t.TempDir()}
	const n = 4
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			files := map[string][]byte{"skill/SKILL.md": []byte(strconv.Itoa(i)), "skill/references/index.md": []byte(strconv.Itoa(i))}
			errs <- s.Publish("r1", 1, files, 2)
		}(i)
	}
	wg.Wait()
	close(errs)
	ok := 0
	for err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, ErrVersionExists):
			t.Fatalf("unexpected err: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d publishers succeeded, want 1", ok)
	}
	a, _ := s.ReadSkillFile("r1", 1, "SKILL.md")
	b, _ := s.ReadSkillFile("r1", 1, "references/index.md")
	if len(a) == 0 || string(a) != string(b) {
		t.Fatalf("mixed version contents: %q vs %q", a, b)
	}
}

func TestStore_RejectsBadIDsAndKeys(t *testing.T) {
	s := Store{Root: t.TempDir()}
	files := map[string][]byte{"skill/SKILL.md": []byte("x")}
	for _, id := range []string{"", "..", "a/b", `a\b`, "a.b", "AUX "} {
		if err := s.Publish(id, 1, files, 1); !errors.Is(err, ErrBadPath) {
			t.Fatalf("Publish(%q) err = %v", id, err)
		}
		if _, err := s.Current(id); !errors.Is(err, ErrBadPath) {
			t.Fatalf("Current(%q) err = %v", id, err)
		}
		if _, err := s.ListSkillFiles(id, 1); !errors.Is(err, ErrBadPath) {
			t.Fatalf("ListSkillFiles(%q) err = %v", id, err)
		}
		if _, err := s.ReadSkillFile(id, 1, "SKILL.md"); !errors.Is(err, ErrBadPath) {
			t.Fatalf("ReadSkillFile(%q) err = %v", id, err)
		}
	}
	keys := []string{"../x", "skill/../../x", "/abs", "skill/a.", "skill//x", ""}
	if runtime.GOOS == "windows" {
		keys = append(keys, "skill/CON")
	}
	for _, key := range keys {
		err := s.Publish("r1", 1, map[string][]byte{key: []byte("x")}, 1)
		if !errors.Is(err, ErrBadPath) {
			t.Fatalf("Publish key %q err = %v", key, err)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Root, "x")); !os.IsNotExist(err) {
		t.Fatal("bad key escaped the store root")
	}
	if err := s.Publish("0b1e-4c_ID", 1, map[string][]byte{"skill/references/areas/a.p2.md": []byte("x")}, 1); err != nil {
		t.Fatal(err)
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
