package main

import "testing"

func TestEvaluateGate_NoBaselinePasses(t *testing.T) {
	if d := EvaluateGate(Summary{Total: 10, Passed: 8}, nil); !d.GatePassed {
		t.Fatal("nil baseline should pass (no gate)")
	}
	if d := EvaluateGate(Summary{}, &Summary{}); !d.GatePassed {
		t.Fatal("empty baseline should pass")
	}
}

func TestEvaluateGate_SmallDropPasses(t *testing.T) {
	baseline := &Summary{Total: 100, Passed: 90, CompletionRate: 0.90}
	current := Summary{Total: 100, Passed: 89, CompletionRate: 0.89} // -1pt
	d := EvaluateGate(current, baseline)
	if !d.GatePassed {
		t.Fatalf("1pt drop should pass, got %+v", d)
	}
	if d.CompletionRateDelta > -0.009 && d.CompletionRateDelta < 0 {
		t.Fatalf("delta should be ~-0.01, got %v", d.CompletionRateDelta)
	}
}

func TestEvaluateGate_LargeDropFails(t *testing.T) {
	baseline := &Summary{Total: 100, Passed: 90, CompletionRate: 0.90}
	current := Summary{Total: 100, Passed: 85, CompletionRate: 0.85} // -5pt
	d := EvaluateGate(current, baseline)
	if d.GatePassed {
		t.Fatalf("5pt drop should fail, got %+v", d)
	}
	if d.CompletionRateDelta > -0.049 {
		t.Fatalf("delta should be ~-0.05, got %v", d.CompletionRateDelta)
	}
}
