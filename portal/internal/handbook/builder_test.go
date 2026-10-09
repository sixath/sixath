package handbook

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBuild(t *testing.T) {
	out, err := Build(context.Background(), BuildInput{RepoID: "r1", RelPath: "cloudgame/svc-a", Root: sampleRepo(t), Commit: "abc", Now: time.Unix(0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"skill/SKILL.md", "skill/references/overview.md", "manifest.json", "coverage.json", "facts/files.json", "facts/symbols.json", "facts/registers.json", "facts/packages.json"} {
		if _, ok := out.Files[p]; !ok {
			t.Fatalf("missing %s", p)
		}
	}
	var man Manifest
	if err := json.Unmarshal(out.Files["manifest.json"], &man); err != nil {
		t.Fatal(err)
	}
	if man.Commit != "abc" || man.GeneratorVersion != GeneratorVersion || man.LeafMode != "file" || man.Stats.Files != 5 {
		t.Fatalf("manifest = %#v", man)
	}
	s := out.Stats
	if s.GoFiles != 3 || s.Packages != 2 || s.Areas != 3 || s.Registers != 1 || s.Symbols == 0 {
		t.Fatalf("stats = %#v", s)
	}
	m := s.Map()
	if m["generator_version"] != GeneratorVersion || m["files"] != float64(5) {
		t.Fatalf("stats map = %#v", m)
	}
	if !strings.Contains(string(out.Files["skill/SKILL.md"]), "name: "+SkillName("cloudgame/svc-a")+"\n") {
		t.Fatal("skill name")
	}
	if s.Cards != 0 || s.StaleCards != 0 || s.Stages != 0 || s.LLMRev != "" {
		t.Fatalf("LLM stats without LLMDir = %#v", s)
	}
}

func TestBuild_UnreadableLLMCacheDegrades(t *testing.T) {
	root := sampleRepo(t)
	facts, err := CollectFacts(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var target File
	for _, f := range facts.Files {
		if CardEligible(f) {
			target = f
			break
		}
	}

	cardErr := LLMCache{Dir: t.TempDir()}
	if err := os.MkdirAll(cardErr.cardPath(target.Hash), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := Build(context.Background(), BuildInput{RelPath: "svc", Root: root, Commit: "c", LLMDir: cardErr.Dir, LLMRev: "7"})
	if err != nil {
		t.Fatalf("unreadable card failed the build: %v", err)
	}
	if out.Stats.Cards != 0 || out.Stats.LLMRev != "7" {
		t.Fatalf("stats %#v", out.Stats)
	}

	skErr := LLMCache{Dir: t.TempDir()}
	if err := skErr.PutCard(target.Hash, &Card{Purpose: "不应渲染", Hash: target.Hash, PromptVersion: LLMPromptVersion}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(skErr.skeletonPath(), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err = Build(context.Background(), BuildInput{RelPath: "svc", Root: root, Commit: "c", LLMDir: skErr.Dir, LLMRev: "7"})
	if err != nil {
		t.Fatalf("unreadable skeleton failed the build: %v", err)
	}
	if out.Stats.Cards != 0 || out.Stats.Stages != 0 || out.Stats.LLMRev != "" {
		t.Fatalf("stats %#v", out.Stats)
	}
	if strings.Contains(string(out.Files["skill/references/overview.md"]), "LLM，") {
		t.Fatal("rendered LLM content from an unreadable cache")
	}
}

func TestBuild_ReadsLLMCache(t *testing.T) {
	root := sampleRepo(t)
	facts, err := CollectFacts(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	cache := LLMCache{Dir: t.TempDir()}
	var target File
	for _, f := range facts.Files {
		if CardEligible(f) {
			target = f
			break
		}
	}
	if target.Path == "" {
		t.Fatal("no card-eligible file in the sample repo")
	}
	if err := cache.PutCard(target.Hash, &Card{Purpose: "缓存里的职责", Hash: target.Hash, PromptVersion: LLMPromptVersion}); err != nil {
		t.Fatal(err)
	}
	sk := &Skeleton{PromptVersion: LLMPromptVersion, Stages: []Stage{{ID: "main", Title: "主流程"}},
		Files: map[string]FileAssign{target.Path: {Stage: "main", CardHash: target.Hash, Hash: target.Hash}}}
	if err := cache.PutSkeleton(sk); err != nil {
		t.Fatal(err)
	}
	out, err := Build(context.Background(), BuildInput{RepoID: "r", RelPath: "svc", Root: root, Commit: "c", LLMDir: cache.Dir, LLMRev: "7"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Stats.Cards != 1 || out.Stats.Stages != 1 || out.Stats.LLMRev != "7" || out.Stats.GeneratorVersion != "p2b-1" {
		t.Fatalf("stats %#v", out.Stats)
	}
	if _, ok := out.Files["skill/references/stages/main.md"]; !ok {
		t.Fatal("stage page not rendered")
	}
	found := false
	for p, b := range out.Files {
		if strings.HasPrefix(p, "skill/references/areas/") && strings.Contains(string(b), "缓存里的职责") {
			found = true
		}
	}
	if !found {
		t.Fatal("card not rendered")
	}
}
