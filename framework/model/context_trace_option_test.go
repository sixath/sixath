package model

import "testing"

func TestWithContextTrace_AppliesToCallConfig(t *testing.T) {
	var called bool
	cfg := ApplyOptions(WithContextTrace(func(string, map[string]any) { called = true }))
	if cfg.ContextTrace == nil {
		t.Fatal("expected ContextTrace set")
	}
	cfg.ContextTrace("x", nil)
	if !called {
		t.Fatal("expected sink invoked")
	}
}

func TestWithMaxContextTokensSoft_AndAlpha_AppliesToCallConfig(t *testing.T) {
	cfg := ApplyOptions(WithMaxContextTokensSoft(1234), WithTokenEstimateAlpha(1.8))
	if cfg.MaxContextTokensSoft != 1234 {
		t.Fatalf("soft tokens mismatch: %d", cfg.MaxContextTokensSoft)
	}
	if cfg.TokenEstimateAlpha != 1.8 {
		t.Fatalf("alpha mismatch: %v", cfg.TokenEstimateAlpha)
	}
}
