package main

import (
	"context"
	"errors"
	"testing"
)

const firstFails = `{"checks":[{"id":1,"pass":false,"reason":"没给清单"},{"id":2,"pass":true,"reason":""},{"id":3,"pass":true,"reason":""},{"id":4,"pass":true,"reason":""}],"attribution":"harness"}`

const firstFailsNoAttr = `{"checks":[{"id":1,"pass":false,"reason":"没给清单"},{"id":2,"pass":true,"reason":""},{"id":3,"pass":true,"reason":""},{"id":4,"pass":true,"reason":""}],"attribution":""}`

func TestScoreAnswerShape(t *testing.T) {
	ts := TraceSummary{Calls: []TraceCall{{Tool: "es_log_query", Hits: 3}}}
	r := scoreAnswerShape(context.Background(), &Judge{Model: &stubJudgeModel{replies: []string{allPass}}}, shapeTask, "198002", ts)
	if !r.Passed || r.Judge == nil || r.Trace == nil || len(r.ToolsUsed) != 1 {
		t.Fatalf("r=%+v", r)
	}
	r = scoreAnswerShape(context.Background(), &Judge{Model: &stubJudgeModel{replies: []string{firstFails}}}, shapeTask, "根因是…", ts)
	if r.Passed || r.FailureReason != "shape_mismatch" || r.Attribution != "harness" {
		t.Fatalf("r=%+v", r)
	}
	r = scoreAnswerShape(context.Background(), &Judge{Model: &stubJudgeModel{replies: []string{firstFailsNoAttr}}}, shapeTask, "根因是…", ts)
	if r.Passed || r.FailureReason != "shape_mismatch" || r.Attribution != "model" {
		t.Fatalf("empty attribution must default to model: %+v", r)
	}
	r = scoreAnswerShape(context.Background(), nil, shapeTask, "x", ts)
	if r.FailureReason != "judge_error" {
		t.Fatalf("nil judge must give judge_error, got %+v", r)
	}
	boom := errors.New("boom")
	r = scoreAnswerShape(context.Background(), &Judge{Model: &stubJudgeModel{errs: []error{boom, boom}}}, shapeTask, "x", ts)
	if r.Passed || r.FailureReason != "judge_error" || r.Error == "" {
		t.Fatalf("judge model error must give judge_error, got %+v", r)
	}
}

func TestMergeRuns(t *testing.T) {
	pass := TaskResult{TaskID: "a", Passed: true, Output: "p"}
	fail := TaskResult{TaskID: "a", FailureReason: "shape_mismatch", Output: "f"}
	lost := TaskResult{TaskID: "a", FailureReason: "infra_error"}

	m := mergeRuns([]TaskResult{pass, fail, pass})
	if !m.Passed || m.Runs != 3 || m.Passes != 2 || m.Output != "p" {
		t.Fatalf("2/3 should pass: %+v", m)
	}
	m = mergeRuns([]TaskResult{pass, fail})
	if m.Passed || m.Runs != 2 || m.Passes != 1 || m.Output != "f" {
		t.Fatalf("1/2 is not a majority: %+v", m)
	}
	m = mergeRuns([]TaskResult{lost, pass, pass})
	if !m.Passed || m.Runs != 2 || m.InfraErrors != 1 {
		t.Fatalf("lost runs excluded from runs: %+v", m)
	}
	m = mergeRuns([]TaskResult{lost, lost})
	if m.Passed || m.Runs != 0 || m.InfraErrors != 2 || m.FailureReason != "infra_error" {
		t.Fatalf("all lost: %+v", m)
	}
}
