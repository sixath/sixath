package handbook

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func hashOf(s string) string { return strings.Repeat(s, 64)[:64] }

func TestCardEligible(t *testing.T) {
	cases := []struct {
		f    File
		want bool
	}{
		{File{Path: "a.go", Lang: "go", Size: 10}, true},
		{File{Path: "empty.go", Lang: "go"}, false},
		{File{Path: "a_test.go", Lang: "go", Size: 10, Test: true}, false},
		{File{Path: "api.proto", Lang: "proto", Size: 10}, true},
		{File{Path: "schema.sql", Lang: "sql", Size: 10}, true},
		{File{Path: "README.md", Lang: "markdown", Size: 10}, false},
		{File{Path: "config.yaml", Lang: "yaml", Size: 10}, false},
		{File{Path: "Makefile", Lang: "", Size: 10}, false},
	}
	for _, c := range cases {
		if got := CardEligible(c.f); got != c.want {
			t.Errorf("%s: got %v", c.f.Path, got)
		}
	}
}

func TestLLMCache_CardsAndSkeleton(t *testing.T) {
	c := LLMCache{Dir: t.TempDir()}
	h := hashOf("a")
	if got, err := c.Card(h); err != nil || got != nil {
		t.Fatalf("missing card: %v %v", got, err)
	}
	card := &Card{Purpose: "处理订单", Role: "service", Hash: h, PromptVersion: LLMPromptVersion}
	if err := c.PutCard(h, card); err != nil {
		t.Fatal(err)
	}
	got, err := c.Card(h)
	if err != nil || got == nil || got.Purpose != "处理订单" {
		t.Fatalf("card: %#v %v", got, err)
	}
	if _, err := c.Card("../x"); err == nil {
		t.Fatal("bad hash must be rejected")
	}
	if sk, err := c.Skeleton(); err != nil || sk != nil {
		t.Fatalf("missing skeleton: %v %v", sk, err)
	}
	sk := &Skeleton{PromptVersion: LLMPromptVersion, BuiltAt: time.Unix(100, 0).UTC(), Stages: []Stage{{ID: "s1", Title: "下单"}},
		Files: map[string]FileAssign{"a.go": {Stage: "s1", CardHash: h}}}
	if err := c.PutSkeleton(sk); err != nil {
		t.Fatal(err)
	}
	back, err := c.Skeleton()
	if err != nil || back == nil || back.Files["a.go"].Stage != "s1" {
		t.Fatalf("skeleton: %#v %v", back, err)
	}
}

func TestLLMCache_PruneCards(t *testing.T) {
	c := LLMCache{Dir: t.TempDir()}
	keep, drop := hashOf("b"), hashOf("c")
	for _, h := range []string{keep, drop} {
		if err := c.PutCard(h, &Card{Purpose: "x", Hash: h}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := c.PruneCards(map[string]bool{keep: true})
	if err != nil || n != 1 {
		t.Fatalf("pruned %d %v", n, err)
	}
	if got, _ := c.Card(keep); got == nil {
		t.Fatal("kept card removed")
	}
	if got, _ := c.Card(drop); got != nil {
		t.Fatal("dropped card still present")
	}
}

func TestLoadLLMLayer_CurrentAndStaleCards(t *testing.T) {
	c := LLMCache{Dir: t.TempDir()}
	oldH, newH, keptH := hashOf("d"), hashOf("e"), hashOf("f")
	_ = c.PutCard(oldH, &Card{Purpose: "旧", Hash: oldH})
	_ = c.PutCard(keptH, &Card{Purpose: "现", Hash: keptH})
	_ = c.PutSkeleton(&Skeleton{PromptVersion: LLMPromptVersion, Stages: []Stage{{ID: "s", Title: "S"}},
		Files: map[string]FileAssign{"changed.go": {Stage: "s", CardHash: oldH}, "kept.go": {Stage: "s", CardHash: keptH}}})
	f := &Facts{Files: []File{
		{Path: "changed.go", Lang: "go", Size: 1, Hash: newH},
		{Path: "kept.go", Lang: "go", Size: 1, Hash: keptH},
		{Path: "kept_test.go", Lang: "go", Size: 1, Hash: keptH, Test: true},
	}}
	l, err := LoadLLMLayer(c, f)
	if err != nil {
		t.Fatal(err)
	}
	if l.Skeleton == nil || l.Cards["kept.go"] == nil || l.Cards["kept_test.go"] != nil {
		t.Fatalf("layer: %#v", l)
	}
	if l.Stale["changed.go"] == nil || l.Stale["changed.go"].Purpose != "旧" || l.Cards["changed.go"] != nil {
		t.Fatalf("stale: %#v", l.Stale)
	}
}

func TestLoadLLMLayer_IgnoresOldPromptSkeleton(t *testing.T) {
	c := LLMCache{Dir: t.TempDir()}
	_ = c.PutSkeleton(&Skeleton{PromptVersion: "old", Stages: []Stage{{ID: "s"}}})
	l, err := LoadLLMLayer(c, &Facts{})
	if err != nil || l.Skeleton != nil || !l.Empty() {
		t.Fatalf("layer %#v %v", l, err)
	}
}

func TestStore_LLMCacheAndReadFacts(t *testing.T) {
	s := Store{Root: t.TempDir()}
	if _, err := s.LLMCache("../x"); err == nil {
		t.Fatal("bad id must be rejected")
	}
	c, err := s.LLMCache("r1")
	if err != nil || c.Dir != filepath.Join(s.Root, "repos", "r1", "llm") {
		t.Fatalf("cache %#v %v", c, err)
	}
	files := map[string][]byte{
		"facts/files.json":     []byte(`[{"path":"a.go","lang":"go","size":3,"lines":1,"hash":"` + hashOf("a") + `"}]`),
		"facts/symbols.json":   []byte(`{"a.go":[{"kind":"func","name":"A","line":1,"end_line":1}]}`),
		"facts/registers.json": []byte(`[]`),
		"facts/packages.json":  []byte(`[{"dir":".","name":"a","files":1}]`),
		"skill/SKILL.md":       []byte("x"),
	}
	if err := s.Publish("r1", 1, files, 2); err != nil {
		t.Fatal(err)
	}
	f, err := s.ReadFacts("r1", 1)
	if err != nil || len(f.Files) != 1 || len(f.Symbols["a.go"]) != 1 || len(f.Packages) != 1 {
		t.Fatalf("facts %#v %v", f, err)
	}
	if f.Module != nil {
		t.Fatalf("missing module.json must leave Module nil: %#v", f.Module)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "repos", "r1", "llm")); !os.IsNotExist(err) {
		t.Fatal("LLMCache must not create directories")
	}
}

func TestBuild_ModuleRoundTripsThroughReadFacts(t *testing.T) {
	out, err := Build(context.Background(), BuildInput{RepoID: "r1", RelPath: "svc", Root: sampleRepo(t), Commit: "abc", Now: time.Unix(0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Files["facts/module.json"]; !ok {
		t.Fatal("Build must write facts/module.json when the repo has go.mod")
	}
	s := Store{Root: t.TempDir()}
	if err := s.Publish("r1", 1, out.Files, 2); err != nil {
		t.Fatal(err)
	}
	f, err := s.ReadFacts("r1", 1)
	if err != nil || f.Module == nil || f.Module.Path != "example.com/svc" || strings.Join(f.Module.Requires, ",") != "github.com/a/b" {
		t.Fatalf("facts module %#v %v", f, err)
	}
}

func TestLLMCache_CorruptFilesAreMisses(t *testing.T) {
	c := LLMCache{Dir: t.TempDir()}
	h := hashOf("a")
	for _, p := range []string{c.cardPath(h), c.skeletonPath()} {
		if err := WriteFileAtomic(p, []byte("{")); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := c.Card(h); err != nil || got != nil {
		t.Fatalf("corrupt card: %#v %v", got, err)
	}
	if sk, err := c.Skeleton(); err != nil || sk != nil {
		t.Fatalf("corrupt skeleton: %#v %v", sk, err)
	}
	for _, p := range []string{c.cardPath(h), c.skeletonPath()} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("corrupt file %s should be removed: %v", p, err)
		}
	}
	for _, p := range []string{c.cardPath(h), c.skeletonPath()} {
		if err := WriteFileAtomic(p, []byte("{")); err != nil {
			t.Fatal(err)
		}
	}
	l, err := LoadLLMLayer(c, &Facts{Files: []File{{Path: "a.go", Lang: "go", Size: 1, Hash: h}}})
	if err != nil || l.Skeleton != nil || len(l.Cards) != 0 {
		t.Fatalf("layer %#v %v", l, err)
	}
}

func TestLLMCache_PutCardRejectsBadHashes(t *testing.T) {
	c := LLMCache{Dir: t.TempDir()}
	for _, h := range []string{"", "../x", strings.ToUpper(hashOf("a")), hashOf("g"), hashOf("a")[:63]} {
		if err := c.PutCard(h, &Card{Purpose: "x"}); !errors.Is(err, errBadHash) {
			t.Errorf("%q: want errBadHash, got %v", h, err)
		}
	}
}

func TestLLMCache_PruneCardsOldPromptVersionAndTempFiles(t *testing.T) {
	c := LLMCache{Dir: t.TempDir()}
	h := hashOf("b")
	if err := c.PutCard(h, &Card{Purpose: "x", Hash: h}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(c.cardPath(h))
	old := filepath.Join(dir, h+"-p0.json")
	freshTmp := filepath.Join(dir, "."+h+"-"+LLMPromptVersion+".json.123")
	staleTmp := filepath.Join(dir, "."+h+"-"+LLMPromptVersion+".json.456")
	for _, p := range []string{old, freshTmp, staleTmp} {
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-2 * staleTmpAge)
	if err := os.Chtimes(staleTmp, past, past); err != nil {
		t.Fatal(err)
	}
	n, err := c.PruneCards(map[string]bool{h: true})
	if err != nil || n != 1 {
		t.Fatalf("pruned %d %v", n, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("card of an old prompt version must be pruned")
	}
	if _, err := os.Stat(freshTmp); err != nil {
		t.Fatal("in-flight temp file must be kept")
	}
	if _, err := os.Stat(staleTmp); !os.IsNotExist(err) {
		t.Fatal("stale temp file must be removed")
	}
	if got, _ := c.Card(h); got == nil {
		t.Fatal("current card removed")
	}
}

func TestLLMCache_ConcurrentPutCard(t *testing.T) {
	c := LLMCache{Dir: t.TempDir()}
	h := hashOf("c")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- c.PutCard(h, &Card{Purpose: "p" + strconv.Itoa(i), Hash: h})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got, err := c.Card(h); err != nil || got == nil || !strings.HasPrefix(got.Purpose, "p") {
		t.Fatalf("card %#v %v", got, err)
	}
}
