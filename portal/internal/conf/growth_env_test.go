package conf

import (
	"os"
	"testing"
)

func TestEnrichGrowthFromEnv(t *testing.T) {
	t.Setenv("SATH_GROWTH_LLM_MODEL", "gpt-test")
	t.Setenv("SATH_GROWTH_LLM_PROVIDER", "openai")
	t.Setenv("SATH_GROWTH_LLM_API_KEY", "k-test")
	g := &Growth{}
	EnrichGrowthFromEnv(g)
	if g.Llm == nil || g.Llm.GetModel() != "gpt-test" {
		t.Fatalf("model: %+v", g.Llm)
	}
	if g.Llm.GetProvider() != "openai" || g.Llm.GetApiKey() != "k-test" {
		t.Fatalf("provider/key: %+v", g.Llm)
	}
}

func TestEnrichGrowthFromEnv_skipsWhenModelInYaml(t *testing.T) {
	os.Unsetenv("SATH_GROWTH_LLM_MODEL")
	g := &Growth{Llm: &GrowthLLM{Model: "yaml-model"}}
	EnrichGrowthFromEnv(g)
	if g.Llm.GetModel() != "yaml-model" {
		t.Fatal("should not override yaml model")
	}
}
