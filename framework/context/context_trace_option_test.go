package context

import "testing"

func TestPipelineConfig_TraceInvokes(t *testing.T) {
	var called bool
	cfg := &PipelineConfig{Trace: func(string, map[string]any) { called = true }}
	if cfg.Trace == nil {
		t.Fatal("expected Trace set")
	}
	cfg.Trace("x", nil)
	if !called {
		t.Fatal("expected sink invoked")
	}
}

func TestPipelineConfig_TokenSoftAndAlpha(t *testing.T) {
	cfg := &PipelineConfig{MaxContextTokensSoft: 1234, TokenEstimateAlpha: 1.8}
	if cfg.MaxContextTokensSoft != 1234 {
		t.Fatalf("soft tokens mismatch: %d", cfg.MaxContextTokensSoft)
	}
	if cfg.TokenEstimateAlpha != 1.8 {
		t.Fatalf("alpha mismatch: %v", cfg.TokenEstimateAlpha)
	}
}
