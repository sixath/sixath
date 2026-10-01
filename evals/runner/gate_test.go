package main

import (
	"strings"
	"testing"
)

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

func shapeSummary(rate, lost float64) *ShapeSummary {
	return &ShapeSummary{Runs: 20, PassRate: rate, LostRate: lost}
}

func TestEvaluateGate_AnswerShape(t *testing.T) {
	base := &Summary{AnswerShape: shapeSummary(0.70, 0)}
	if d := EvaluateGate(Summary{AnswerShape: shapeSummary(0.65, 0)}, base); !d.GatePassed {
		t.Fatalf("5pt drop is within 8pt threshold: %+v", d.AnswerShape)
	}
	d := EvaluateGate(Summary{AnswerShape: shapeSummary(0.60, 0)}, base)
	if d.GatePassed || d.AnswerShape.GatePassed {
		t.Fatalf("10pt drop must fail: %+v", d.AnswerShape)
	}
	if err := gateError(d); err == nil || !strings.Contains(err.Error(), "e2e_pass_rate") {
		t.Fatalf("gateError=%v", err)
	}
}

func TestEvaluateGate_AnswerShapeInvalidWithoutBaseline(t *testing.T) {
	d := EvaluateGate(Summary{AnswerShape: shapeSummary(0.9, 0.25)}, nil)
	if d.GatePassed || !d.AnswerShape.Invalid {
		t.Fatalf(">20%% lost runs must invalidate the run: %+v", d.AnswerShape)
	}
}

func TestEvaluateGate_ShapeOnlyRunIgnoresCompletionBaseline(t *testing.T) {
	base := &Summary{Total: 48, CompletionRate: 0.9}
	if d := EvaluateGate(Summary{AnswerShape: shapeSummary(0.5, 0)}, base); !d.GatePassed {
		t.Fatalf("run without legacy tasks must not fail the completion gate: %+v", d)
	}
}

func TestTaskRegressions(t *testing.T) {
	base := []TaskResult{{TaskID: "a", Passed: true}, {TaskID: "b", Passed: true}, {TaskID: "c"}, {TaskID: "e", Passed: true}}
	cur := []TaskResult{{TaskID: "a", Passed: true}, {TaskID: "b"}, {TaskID: "c"}, {TaskID: "d"}, {TaskID: "e", FailureReason: "infra_error"}}
	got := TaskRegressions(cur, base)
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("got %v", got)
	}
	d := &Diff{GatePassed: true}
	d.applyRegressions(got)
	if d.GatePassed || d.AnswerShape == nil || len(d.AnswerShape.Regressed) != 1 {
		t.Fatalf("d=%+v", d)
	}
	if err := gateError(d); err == nil || !strings.Contains(err.Error(), "b") {
		t.Fatalf("gateError=%v", err)
	}
}