package chat

import "testing"

func TestEstimateCost(t *testing.T) {
	// gpt-4o：in $2.50 / out $10.00（每 1M）
	if c := EstimateCost("gpt-4o", 1_000_000, 0); c != 2.50 {
		t.Fatalf("gpt-4o input cost = %v, want 2.50", c)
	}
	if c := EstimateCost("gpt-4o", 0, 1_000_000); c != 10.00 {
		t.Fatalf("gpt-4o output cost = %v, want 10.00", c)
	}
	if c := EstimateCost("GPT-4O", 1_000_000, 1_000_000); c != 12.50 {
		t.Fatalf("gpt-4o mixed cost = %v, want 12.50", c)
	}
	// 未匹配模型用兜底价（$1.00 / $4.00）。
	if c := EstimateCost("unknown-model", 1_000_000, 0); c != 1.00 {
		t.Fatalf("fallback input cost = %v, want 1.00", c)
	}
	// 无用量 → 0。
	if c := EstimateCost("gpt-4o", 0, 0); c != 0 {
		t.Fatalf("zero usage cost = %v, want 0", c)
	}
}
