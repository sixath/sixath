package model

import "testing"

func TestEstimateTokensConservative_alpha(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "你好"}}
	n1 := EstimateTokensConservative(msgs, 1.0)
	if n1 != 2 {
		t.Fatalf("alpha=1 want 2 runes, got %d", n1)
	}
	n2 := EstimateTokensConservative(msgs, 2.0)
	if n2 != 4 {
		t.Fatalf("alpha=2 want ceil 4, got %d", n2)
	}
}
