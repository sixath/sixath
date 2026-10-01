package main

import (
	"math"
	"testing"
)

func TestComputeSummary_Basics(t *testing.T) {
	tasks := []Task{
		{ID: "s1", Category: "single_tool", Expect: Expectation{Tools: []string{"list_tables"}}},
		{ID: "s2", Category: "single_tool", Expect: Expectation{Tools: []string{"execute_read"}}},
	}
	results := []TaskResult{
		{TaskID: "s1", Category: "single_tool", Passed: true, Steps: 1, ToolsUsed: []string{"list_tables"}},
		{TaskID: "s2", Category: "single_tool", Passed: false, Steps: 3, ToolsUsed: []string{"web_search"}, FailureReason: "wrong_tool", Attribution: "tool"},
	}

	s := ComputeSummary(results, tasks)
	if s.Total != 2 || s.Passed != 1 {
		t.Fatalf("total/passed = %d/%d", s.Total, s.Passed)
	}
	if math.Abs(s.CompletionRate-0.5) > 1e-9 {
		t.Fatalf("completion_rate = %v, want 0.5", s.CompletionRate)
	}
	if math.Abs(s.AvgSteps-2.0) > 1e-9 {
		t.Fatalf("avg_steps = %v, want 2.0", s.AvgSteps)
	}
	if s.Attribution["tool"] != 1 {
		t.Fatalf("attribution = %v", s.Attribution)
	}
}

func TestComputeSummary_SetF1(t *testing.T) {
	// 期望 {a,b}，实际 {a}：precision=1, recall=0.5, F1=2/3≈0.6667
	tasks := []Task{{ID: "t", Category: "single_tool", Expect: Expectation{Tools: []string{"a", "b"}}}}
	results := []TaskResult{{TaskID: "t", Category: "single_tool", Passed: true, Steps: 1, ToolsUsed: []string{"a"}}}

	s := ComputeSummary(results, tasks)
	if math.Abs(s.ToolF1-2.0/3.0) > 1e-6 {
		t.Fatalf("tool_f1 = %v, want ~0.6667", s.ToolF1)
	}
}

func TestComputeSummary_SequenceF1(t *testing.T) {
	// 期望序列 [a,b,c]，实际 [a,b]：match=2, p=1, r=2/3, F1=0.8
	tasks := []Task{{ID: "t", Category: "multi_tool", Expect: Expectation{ToolSequence: []string{"a", "b", "c"}}}}
	results := []TaskResult{{TaskID: "t", Category: "multi_tool", Passed: true, Steps: 2, ToolsUsed: []string{"a", "b"}}}

	s := ComputeSummary(results, tasks)
	if math.Abs(s.ToolF1-0.8) > 1e-6 {
		t.Fatalf("tool_f1 = %v, want 0.8", s.ToolF1)
	}
}

func TestComputeSummary_NoExpectedToolsSkipsF1(t *testing.T) {
	// 无期望工具的类别（如 safety）不参与 F1 统计。
	tasks := []Task{{ID: "t", Category: "safety", Expect: Expectation{MustRefuse: []string{"execute_write"}}}}
	results := []TaskResult{{TaskID: "t", Category: "safety", Passed: true, Steps: 1, ToolsUsed: []string{"web_search"}}}

	s := ComputeSummary(results, tasks)
	if s.ToolF1 != 0 {
		t.Fatalf("tool_f1 = %v, want 0 (no expected tools)", s.ToolF1)
	}
}

func TestComputeSummary_ByCategory(t *testing.T) {
	results := []TaskResult{
		{TaskID: "a", Category: "hitl", Passed: true, Steps: 1},
		{TaskID: "b", Category: "hitl", Passed: false, Steps: 2},
		{TaskID: "c", Category: "safety", Passed: true, Steps: 1},
	}
	s := ComputeSummary(results, nil)
	hitl := s.ByCategory["hitl"]
	if hitl.Total != 2 || hitl.Passed != 1 || math.Abs(hitl.Rate-0.5) > 1e-9 {
		t.Fatalf("hitl stat = %+v", hitl)
	}
}

func TestComputeSummary_Empty(t *testing.T) {
	s := ComputeSummary(nil, nil)
	if s.Total != 0 {
		t.Fatalf("empty summary total = %d", s.Total)
	}
}

func TestComputeSummary_AnswerShapeSeparated(t *testing.T) {
	tasks := []Task{
		{ID: "s1", Category: "answer_shape", Expect: Expectation{AnswerType: "enumerate"}},
		{ID: "s2", Category: "answer_shape", Expect: Expectation{AnswerType: "diagnose"}},
		{ID: "s3", Category: "answer_shape", Expect: Expectation{AnswerType: "count"}},
		{ID: "t1", Category: "single_tool", Expect: Expectation{Tools: []string{"list_tables"}}},
	}
	results := []TaskResult{
		{TaskID: "s1", Category: "answer_shape", Passed: true, Runs: 2, Passes: 2, Steps: 3,
			Trace: &TraceSummary{Calls: []TraceCall{{Tool: "a", Hits: 1}, {Tool: "b", Empty: true}}}},
		{TaskID: "s2", Category: "answer_shape", Runs: 2, Passes: 1, Steps: 5, Attribution: "harness", FailureReason: "shape_mismatch",
			Judge: &JudgeVerdict{Checks: []JudgeCheck{{ID: 1, Pass: true}, {ID: 2, Pass: true}, {ID: 3, Pass: true}, {ID: 4, Pass: false}}},
			Trace: &TraceSummary{Calls: []TraceCall{{Tool: "c", Error: "boom"}}}},
		{TaskID: "s3", Category: "answer_shape", FailureReason: "infra_error", InfraErrors: 2},
		{TaskID: "t1", Category: "single_tool", Passed: true, Steps: 2, ToolsUsed: []string{"list_tables"}},
	}
	s := ComputeSummary(results, tasks)
	if s.Total != 1 || s.CompletionRate != 1 {
		t.Fatalf("answer_shape must not count toward completion_rate: %+v", s)
	}
	if _, ok := s.ByCategory["answer_shape"]; ok || s.Attribution["harness"] != 0 {
		t.Fatalf("answer_shape leaked into legacy by_category/attribution: %+v", s)
	}
	a := s.AnswerShape
	if a == nil {
		t.Fatal("missing answer_shape summary")
	}
	if a.Tasks != 3 || a.Runs != 4 || a.Passes != 3 || a.LostRuns != 2 {
		t.Fatalf("counts %+v", a)
	}
	if a.PassRate != 0.75 || a.LostRate != 2.0/6 {
		t.Fatalf("rates pass=%v lost=%v", a.PassRate, a.LostRate)
	}
	if a.ByAnswerType["diagnose"].Total != 2 || a.ByAnswerType["diagnose"].Passed != 1 {
		t.Fatalf("by type %+v", a.ByAnswerType)
	}
	if a.Rule4Failures != 1 || a.Attribution["harness"] != 1 {
		t.Fatalf("rule4=%d attribution=%v", a.Rule4Failures, a.Attribution)
	}
	if a.ToolErrorRate != 1.0/3 || a.EmptyRate != 1.0/3 {
		t.Fatalf("tool rates err=%v empty=%v", a.ToolErrorRate, a.EmptyRate)
	}
}

func TestComputeSummary_OnlyAnswerShapeNoNaN(t *testing.T) {
	s := ComputeSummary([]TaskResult{{TaskID: "s1", Category: "answer_shape", Passed: true, Runs: 1, Passes: 1}}, nil)
	if s.Total != 0 || s.CompletionRate != 0 || s.AnswerShape == nil || s.AnswerShape.PassRate != 1 {
		t.Fatalf("s=%+v", s)
	}
}