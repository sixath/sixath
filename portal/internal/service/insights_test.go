package service

import (
	"testing"
	"time"

	agent "github.com/sixath/framework/harness"
)

func TestAggregateInsights_TopToolsAndErrorRate(t *testing.T) {
	traces := []agent.TurnTrace{
		{
			SessionID: "s1",
			Calls: []agent.TurnToolCall{
				{ToolName: "terminal", ResultPreview: "ok"},
				{ToolName: "terminal", Error: "boom"},
				{ToolName: "jaeger_trace", Error: "missing"},
			},
		},
		{
			SessionID: "s2",
			Calls: []agent.TurnToolCall{
				{ToolName: "terminal", ResultPreview: "ok"},
				{ToolName: "execute_read", Blocked: true},
			},
		},
	}
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	rep := AggregateInsights(traces, "agent-1", from, to, false)
	if rep.Turns != 2 || rep.ToolCalls != 5 || rep.ErrorCalls != 2 || rep.BlockedCalls != 1 {
		t.Fatalf("counts turns=%d tools=%d err=%d blocked=%d", rep.Turns, rep.ToolCalls, rep.ErrorCalls, rep.BlockedCalls)
	}
	if rep.ErrorRate < 0.39 || rep.ErrorRate > 0.41 {
		t.Fatalf("error_rate=%v want ~0.4", rep.ErrorRate)
	}
	if len(rep.TopTools) < 1 || rep.TopTools[0].Name != "terminal" || rep.TopTools[0].Calls != 3 {
		t.Fatalf("top_tools=%+v", rep.TopTools)
	}
	if len(rep.TopSessions) < 1 {
		t.Fatal("expected top sessions")
	}
}
