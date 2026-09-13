package chat

import (
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/agent"
)

func TestTokenCounterKey(t *testing.T) {
	tests := []struct {
		provider, model, want string
	}{
		{"", "", "default"},
		{"OpenAI", "GPT-4o", "openai/GPT-4o"},
		{" openai ", " gpt-4o ", "openai/gpt-4o"},
		{"openai", "", "openai"},
		{"", "gpt-4o", "gpt-4o"},
	}
	for _, tt := range tests {
		if got := TokenCounterKey(tt.provider, tt.model); got != tt.want {
			t.Fatalf("TokenCounterKey(%q,%q)=%q want %q", tt.provider, tt.model, got, tt.want)
		}
	}
}

func TestTokenCounterFor_ReusesPerModelCounter(t *testing.T) {
	a := TokenCounterFor("openai", "gpt-4o")
	b := TokenCounterFor("openai", "gpt-4o")
	if a != b {
		t.Fatal("same provider/model must share one counter")
	}
	other := TokenCounterFor("dashscope", "qwen-plus")
	if a == other {
		t.Fatal("different models must not share a counter")
	}
	if _, ok := a.(interface{ Observe(int, int) }); !ok {
		t.Fatalf("portal counter must be the calibratable implementation, got %T", a)
	}
}

func TestTokenCounterFor_CalibrationIsVisibleInStats(t *testing.T) {
	counter := TokenCounterFor("openai", "stats-probe-model")
	type observer interface{ Observe(estimated, actual int) }
	obs, ok := counter.(observer)
	if !ok {
		t.Fatalf("counter %T is not calibratable", counter)
	}
	obs.Observe(200, 100)

	stats, ok := TokenCounterStats()["openai/stats-probe-model"]
	if !ok {
		t.Fatalf("stats missing key: %#v", TokenCounterStats())
	}
	if stats.Samples != 1 {
		t.Fatalf("Samples=%d want 1", stats.Samples)
	}
	if stats.Alpha >= stats.BaseAlpha {
		t.Fatalf("alpha should have been corrected downwards: %+v", stats)
	}
}

func TestReActOptionsFromAgent_InjectsTokenCounter(t *testing.T) {
	meta := biz.AgentMeta{
		ModelConfig: biz.ModelConfig{Provider: "openai", Model: "gpt-4o"},
	}
	cfg := agent.ReActConfig{}
	for _, opt := range ReActOptionsFromAgent(meta) {
		opt(&cfg)
	}
	if cfg.TokenCounter == nil {
		t.Fatal("expected TokenCounter to be injected")
	}
	if cfg.MaxOutputTokens != 0 {
		t.Fatalf("MaxOutputTokens=%d want 0 when unset (BuildReActAgent applies its default)", cfg.MaxOutputTokens)
	}
}

func TestReActOptionsFromAgent_AppliesMaxOutputTokens(t *testing.T) {
	meta := biz.AgentMeta{
		ModelConfig: biz.ModelConfig{Provider: "dashscope", Model: "qwen-plus", MaxOutputTokens: 4096},
	}
	cfg := agent.ReActConfig{}
	for _, opt := range ReActOptionsFromAgent(meta) {
		opt(&cfg)
	}
	if cfg.TokenCounter == nil {
		t.Fatal("expected TokenCounter to be injected even when max_output_tokens is set")
	}
	if cfg.MaxOutputTokens != 4096 {
		t.Fatalf("MaxOutputTokens=%d want 4096", cfg.MaxOutputTokens)
	}
}
