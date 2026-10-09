package handbook

import (
	"context"
	"encoding/json"
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
}
