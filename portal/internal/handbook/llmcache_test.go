package handbook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func hashOf(s string) string { return strings.Repeat(s, 64)[:64] }

func TestCardEligible(t *testing.T) {
	cases := []struct {
		f    File
		want bool
	}{
		{File{Path: "a.go", Lang: "go"}, true},
		{File{Path: "a_test.go", Lang: "go", Test: true}, false},
		{File{Path: "api.proto", Lang: "proto"}, true},
		{File{Path: "schema.sql", Lang: "sql"}, true},
		{File{Path: "README.md", Lang: "markdown"}, false},
		{File{Path: "config.yaml", Lang: "yaml"}, false},
		{File{Path: "Makefile", Lang: ""}, false},
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
		{Path: "changed.go", Lang: "go", Hash: newH},
		{Path: "kept.go", Lang: "go", Hash: keptH},
		{Path: "kept_test.go", Lang: "go", Hash: keptH, Test: true},
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
	if _, err := os.Stat(filepath.Join(s.Root, "repos", "r1", "llm")); !os.IsNotExist(err) {
		t.Fatal("LLMCache must not create directories")
	}
}
