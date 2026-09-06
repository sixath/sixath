package conf

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v2"
)

type shippedGrowthYAML struct {
	Growth struct {
		LlmReviewEnabled               bool `yaml:"llm_review_enabled"`
		WorkerEnabled                  bool `yaml:"worker_enabled"`
		CombinedReviewEnabled          bool `yaml:"combined_review_enabled"`
		SessionEndMemoryReviewEnabled  bool `yaml:"session_end_memory_review_enabled"`
		SessionEndSkillReviewEnabled   bool `yaml:"session_end_skill_review_enabled"`
		CuratorEnabled                 bool `yaml:"curator_enabled"`
		LearningsReviewEnabled         bool `yaml:"learnings_review_enabled"`
	} `yaml:"growth"`
}

func TestShippedConfig_omitsDeadGrowthWorkerKeys(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "configs")
	needles := []string{
		"worker_enabled",
		"llm_review_enabled",
		"curator_enabled",
		"learnings_review_enabled",
		"combined_review_enabled",
		"session_end_memory_review_enabled",
		"session_end_skill_review_enabled",
	}
	for _, name := range []string{"config.yaml", "config.docker.yaml"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		src := string(data)
		if !strings.Contains(src, "growth:") {
			t.Errorf("%s must keep a growth: section for /route llm", name)
		}
		for _, needle := range needles {
			if strings.Contains(src, needle) {
				t.Errorf("%s must not ship dead growth key %q", name, needle)
			}
		}
	}
}

func TestShippedConfig_growthReviewFlagsOff(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "configs")
	for _, name := range []string{"config.yaml", "config.docker.yaml"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var raw shippedGrowthYAML
		if err := yaml.Unmarshal(data, &raw); err != nil {
			t.Fatalf("unmarshal %s: %v", name, err)
		}
		g := raw.Growth
		if g.LlmReviewEnabled {
			t.Errorf("%s llm_review_enabled=true, want false", name)
		}
		if g.SessionEndSkillReviewEnabled {
			t.Errorf("%s session_end_skill_review_enabled=true, want false", name)
		}
		if g.LearningsReviewEnabled {
			t.Errorf("%s learnings_review_enabled=true, want false", name)
		}
		if g.WorkerEnabled {
			t.Errorf("%s worker_enabled=true, want false", name)
		}
		if g.CuratorEnabled {
			t.Errorf("%s curator_enabled=true, want false", name)
		}
		if g.SessionEndMemoryReviewEnabled {
			t.Errorf("%s session_end_memory_review_enabled=true, want false", name)
		}
		if g.CombinedReviewEnabled {
			t.Errorf("%s combined_review_enabled=true, want false", name)
		}
	}
}

func TestShippedConfig_skillAutoRouteOff(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "configs")
	needles := []string{"auto_route_enabled", "route_min_score", "route_max_body_runes"}
	for _, name := range []string{"config.yaml", "config.docker.yaml"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		src := string(data)
		for _, needle := range needles {
			if strings.Contains(src, needle) {
				t.Errorf("%s must not ship dead skill route key %q", name, needle)
			}
		}
	}
}
