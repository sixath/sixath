package conf

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

func TestAgentProto_omitsDeadKeys(t *testing.T) {
	p := filepath.Join(repoRoot(t), "portal", "api", "agent", "v1", "agent.proto")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, needle := range []string{
		"code_provider",
		"code_model",
		"code_api_key",
		"code_base_url",
		"mea_enabled",
		"hub_governance",
		"hub_knowledge",
		"hub_fallback_to_default_on_read_error",
	} {
		if strings.Contains(src, needle) {
			t.Errorf("agent.proto must not declare %q", needle)
		}
	}
	if !strings.Contains(src, "hybrid_recall") {
		t.Fatal("hybrid_recall must remain")
	}
}

func TestConfProto_omitsDeadGrowthAndSkillRouteKeys(t *testing.T) {
	p := filepath.Join(repoRoot(t), "portal", "internal", "conf", "conf.proto")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, needle := range []string{
		"worker_enabled",
		"llm_review_enabled",
		"curator_enabled",
		"learnings_review_enabled",
		"auto_route_enabled",
		"route_min_score",
		"route_max_body_runes",
		"skill_system_prompt",
		"curator_system_prompt",
	} {
		if strings.Contains(src, needle) {
			t.Errorf("conf.proto must not declare %q", needle)
		}
	}
	if !strings.Contains(src, "GrowthLLM llm") && !strings.Contains(src, "GrowthLLM llm = 5") {
		// field remains llm = 5
		if !strings.Contains(src, "llm = 5") {
			t.Fatal("Growth.llm must remain")
		}
	}
	if !strings.Contains(src, "allow_script_execution") {
		t.Fatal("Skills.allow_script_execution must remain")
	}
}
