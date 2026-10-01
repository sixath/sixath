package main

import (
	"strings"
	"testing"

	"github.com/sixath/framework/executor"
	agent "github.com/sixath/framework/harness"
)

func TestNewTraceCallResultShapedError(t *testing.T) {
	cases := []struct {
		name    string
		result  any
		wantErr string
	}{
		{"ok false", map[string]any{"ok": false, "error": "index not found", "hit_status": "error"}, "index not found"},
		{"hit_status error no message", map[string]any{"hit_status": "error"}, "hit_status=error"},
		{"json string", `{"ok":false,"error":"bad query","error_code":"QUERY_INVALID","hit_status":"error"}`, "bad query"},
		{"truncated", `{"ok":false,"error":"boom","hit_status":"error","detail":"` + "…[truncated]", "boom"},
		{"ok false no message", map[string]any{"ok": false}, "ok=false"},
		{"truncated ok false no message", `{"ok":false,"detail":"` + "…[truncated]", "ok=false"},
		{"truncated hit_status error no message", `{"hit_status":"error","detail":"` + "…[truncated]", "hit_status=error"},
	}
	for _, c := range cases {
		s := TraceSummary{Calls: []TraceCall{newTraceCall("es_log_query", nil, c.result, "")}}
		if s.ErrorCount() != 1 || s.Calls[0].Error != c.wantErr || s.EmptyCount() != 0 {
			t.Errorf("%s: calls=%+v", c.name, s.Calls)
		}
	}
}

func TestNewTraceCallTruncatedHitsIgnoresNestedOKFalse(t *testing.T) {
	c := newTraceCall("es_log_query", nil, `{"hit_status":"hits","hits":[{"ok":false,`+"…[truncated]", "")
	if c.Error != "" || c.Empty {
		t.Fatalf("call=%+v", c)
	}
}

func TestNewTraceCallRedactsArgs(t *testing.T) {
	c := newTraceCall("ssh_exec", map[string]any{"host": "10.0.0.1", "password": "hunter2", "command": "sshpass -p s3cret ssh x"}, "ok", "")
	if strings.Contains(c.Args, "hunter2") || strings.Contains(c.Args, "s3cret") {
		t.Fatalf("args not redacted: %s", c.Args)
	}
	if !strings.Contains(c.Args, "10.0.0.1") {
		t.Fatalf("args lost non-secret data: %s", c.Args)
	}
}

func TestResultHits(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want int
	}{
		{"nil", nil, -1},
		{"es total", map[string]any{"total": float64(12)}, 12},
		{"es empty", map[string]any{"total": float64(0), "hit_status": "empty"}, 0},
		{"rows", map[string]any{"rows": []any{1, 2}}, 2},
		{"typed nested rows", map[string]any{"rows": []map[string]any{{}}}, 1},
		{"query result", &executor.QueryResult{Rows: [][]any{{1}}, HitStatus: "hits"}, 1},
		{"truncated total", `{"ok":true,"hit_status":"hits","total":42,"hits":[{"a":` + "…[truncated]", 42},
		{"truncated empty", `{"hit_status":"empty","rows":[` + "…[truncated]", 0},
		{"truncated unknown", `{"ok":true,"hits":[{"a":` + "…[truncated]", -1},
		{"json string", `{"ok":true,"total":0}`, 0},
		{"plain string", "ok", -1},
		{"empty string", "", 0},
		{"array", []any{1}, 1},
		{"no count keys", map[string]any{"ok": true}, -1},
	}
	for _, c := range cases {
		if got := resultHits(c.in); got != c.want {
			t.Errorf("%s: resultHits=%d want %d", c.name, got, c.want)
		}
	}
}

func TestSummarizeRunTrace(t *testing.T) {
	tr := &agent.RunTrace{ToolCalls: []agent.ToolCallRecord{
		{ToolName: "es_log_query", Arguments: map[string]any{"query": "vmId:1"}, Result: `{"total":0,"hit_status":"empty"}`},
		{ToolName: "execute_read", Error: "syntax error"},
		{ToolName: "es_log_query", Result: map[string]any{"total": float64(3)}},
		{ToolName: "es_log_query", Result: map[string]any{"ok": false, "error": "index not found", "hit_status": "error"}},
	}}
	s := summarizeRunTrace(tr)
	if len(s.Calls) != 4 || s.EmptyCount() != 1 || s.ErrorCount() != 2 || s.Calls[3].Error != "index not found" {
		t.Fatalf("summary=%+v empty=%d err=%d", s, s.EmptyCount(), s.ErrorCount())
	}
	if s.Calls[0].Args != `{"query":"vmId:1"}` {
		t.Fatalf("args=%q", s.Calls[0].Args)
	}
	if got := summarizeRunTrace(nil); len(got.Calls) != 0 {
		t.Fatal("nil trace must give empty summary")
	}
}

func TestNewTraceCallAggregationEmpty(t *testing.T) {
	cases := []struct {
		name     string
		args     any
		result   any
		wantAggE bool
	}{
		{"agg_field empty buckets", map[string]any{"agg_field": "vmId"}, map[string]any{"total": float64(120), "aggregations": map[string]any{"by_field": map[string]any{"buckets": []any{}}}}, true},
		{"json string result", map[string]any{"agg_field": "vmId"}, `{"total":120,"aggregations":{"by_field":{"buckets":[]}}}`, true},
		{"agg_interval missing aggregations", map[string]any{"agg_interval": "1m"}, map[string]any{"total": float64(5)}, false},
		{"truncated empty buckets", map[string]any{"agg_field": "vmId"}, `{"aggregations":{"by_field":{"buckets":[]}},"cluster":"c","hits":[{"a":` + portalTruncatedSuffix, true},
		{"truncated non-empty buckets", map[string]any{"agg_field": "vmId"}, `{"aggregations":{"by_field":{"buckets":[{"key":"vm-1","count":3}]}},"cluster":"c","hits":[{"a":` + portalTruncatedSuffix, false},
		{"truncated aggregations cut off", map[string]any{"agg_field": "vmId"}, `{"aggregations":{"by_field":{"buckets":[` + portalTruncatedSuffix, false},
		{"all present buckets empty", map[string]any{"agg_field": "vmId", "agg_interval": "1m"}, `{"total":9,"aggregations":{"by_field":{"buckets":[]},"timeline":{"buckets":[]}}}`, true},
		{"aggs in json body", map[string]any{"query": `{"query":{"match_all":{}},"aggs":{"x":{"terms":{"field":"vmId"}}}}`}, `{"total":3,"aggregations":{"x":{"buckets":[]}}}`, true},
		{"one bucket non-empty", map[string]any{"agg_field": "vmId", "agg_interval": "1m"}, `{"total":9,"aggregations":{"by_field":{"buckets":[]},"timeline":{"buckets":[{"key":"t","count":9}]}}}`, false},
		{"buckets present", map[string]any{"agg_field": "host.keyword"}, `{"total":40,"aggregations":{"by_field":{"buckets":[{"key":"vm-1","count":40}]}}}`, false},
		{"no agg requested", map[string]any{"query": "vmId:1"}, `{"total":3}`, false},
		{"metric aggs only", map[string]any{"query": `{"aggs":{"m":{"max":{"field":"x"}}}}`}, `{"total":3,"aggregations":{"m":{"value":1}}}`, false},
		{"truncated result unknown", map[string]any{"agg_field": "vmId"}, `{"ok":true,"total":42,"hits":[{"a":` + "…[truncated]", false},
	}
	for _, c := range cases {
		got := newTraceCall("es_log_query", c.args, c.result, "")
		if got.AggEmpty != c.wantAggE || (c.wantAggE && !got.Empty) {
			t.Errorf("%s: call=%+v", c.name, got)
		}
	}
}

func TestNewTraceCallTruncatesError(t *testing.T) {
	c := newTraceCall("x", nil, nil, strings.Repeat("e", 2000))
	if n := len([]rune(c.Error)); n > traceErrorMaxRunes+1 {
		t.Fatalf("error not truncated: %d runes", n)
	}
	r := newTraceCall("x", nil, map[string]any{"ok": false, "error": strings.Repeat("错", 2000)}, "")
	if n := len([]rune(r.Error)); n > traceErrorMaxRunes+1 {
		t.Fatalf("result error not truncated: %d runes", n)
	}
}

func TestSummarizeTimeline(t *testing.T) {
	tl := []any{
		map[string]any{"kind": "model", "step": float64(0), "phase": "responded"},
		map[string]any{"kind": "tool", "toolName": "es_log_query", "phase": "completed", "arguments": map[string]any{"q": "x"}, "result": map[string]any{"total": float64(0)}},
		map[string]any{"kind": "tool", "toolName": "ssh_exec", "phase": "failed"},
		map[string]any{"kind": "tool", "toolName": "terminal", "phase": "interrupted"},
	}
	s := summarizeTimeline(tl)
	if len(s.Calls) != 3 {
		t.Fatalf("want 3 tool calls, got %+v", s.Calls)
	}
	if !s.Calls[0].Empty || s.Calls[1].Error != "failed" || s.Calls[2].Error != "interrupted" {
		t.Fatalf("calls=%+v", s.Calls)
	}
}
