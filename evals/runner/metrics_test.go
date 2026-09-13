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