package agent

import (
	"strings"
	"testing"

	"github.com/sixath/framework/tool"
)

func TestEvaluateTruncatedPageGate_SpillStub(t *testing.T) {
	q := "请解析全部日志并统计分布"
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query",
		Result: &tool.QuerySpillStub{
			Spilled:      true,
			HasMore:      true,
			ContinueFrom: 50,
		},
	}}}
	got := EvaluateTruncatedPageGate(tr, q)
	if got.Allow {
		t.Fatal("spilled stub with has_more must inject, not allow finish")
	}
	if got.Action != "inject" {
		t.Fatalf("action=%q want inject", got.Action)
	}
	if got.Reason != truncatedPageReason {
		t.Fatalf("reason=%q want %q", got.Reason, truncatedPageReason)
	}
	if !strings.Contains(got.Prompt, "50") {
		t.Fatalf("prompt should name continue_from, got %q", got.Prompt)
	}
	if !strings.Contains(got.Prompt, "result_stats") {
		t.Fatalf("prompt should mention result_stats, got %q", got.Prompt)
	}
}

func TestEvaluateTruncatedPageGate_SpillWithoutMore(t *testing.T) {
	q := "解析全部剩余日志"
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query",
		Result: &tool.QuerySpillStub{
			Spilled: true,
			Path:    "tmp/results/sess/page.jsonl",
			Count:   12,
		},
	}}}
	got := EvaluateTruncatedPageGate(tr, q)
	if !got.Allow {
		t.Fatalf("spilled stub without more must allow, got %#v", got)
	}
}

func TestEvaluateTruncatedPageGate_WantsCompleteSet(t *testing.T) {
	q := "查询最近7天 DiscardUserArchive 并解析 args 的 flowid，再用这些 flowId 查 vmid 和 gid"
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query",
		Result: map[string]any{
			"truncated":     true,
			"has_more":      true,
			"total":         432,
			"count":         432,
			"from":          0,
			"next_from":     50,
			"continue_from": 50,
			"hits":          []any{1, 2},
		},
	}}}
	got := EvaluateTruncatedPageGate(tr, q)
	if got.Allow {
		t.Fatal("partial page must not be treated as done")
	}
	if got.Action != "inject" {
		t.Fatalf("action=%q", got.Action)
	}
	if got.Reason != truncatedPageReason {
		t.Fatalf("reason=%q want %q", got.Reason, truncatedPageReason)
	}
	if !strings.Contains(got.Prompt, "50") {
		t.Fatalf("prompt should name continue_from, got %q", got.Prompt)
	}
}

func TestEvaluateTruncatedPageGate_SampleQueryAllowsFinish(t *testing.T) {
	q := "随便看一条 DiscardUserArchive 日志长什么样"
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query",
		Result:   map[string]any{"truncated": true, "continue_from": 50, "count": 432},
	}}}
	got := EvaluateTruncatedPageGate(tr, q)
	if !got.Allow {
		t.Fatalf("exploratory sample must be allowed to finish, prompt=%q", got.Prompt)
	}
}
