package service

import (
	"strings"
	"testing"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/tool"
)

func TestToolCallPayloadFromRecord_MapsFields(t *testing.T) {
	rec := agent.ToolCallRecord{
		Step:       2,
		ToolCallID: "call_1",
		ToolName:   "execute_query",
		Arguments:  map[string]any{"sql": "SELECT 1"},
		Result:     map[string]any{"rows": 42},
		Allowed:    true,
		Decision:   "allowed",
		DurationMS: 128,
	}
	p := toolCallPayloadFromRecord(rec, "completed")
	if p.ID != "call_1" || p.Step != 2 || p.ToolName != "execute_query" {
		t.Fatalf("basic fields wrong: %+v", p)
	}
	if p.Phase != "completed" || p.DurationMS != 128 || !p.Allowed {
		t.Fatalf("status fields wrong: %+v", p)
	}
}

func TestToolCallPayloadFromRecord_TruncatesLargeResult(t *testing.T) {
	big := strings.Repeat("x", 20*1024) // 20KB > 8KB 上限
	rec := agent.ToolCallRecord{
		ToolCallID: "call_2",
		ToolName:   "read_file",
		Result:     big,
	}
	p := toolCallPayloadFromRecord(rec, "completed")
	if !p.Truncated {
		t.Fatal("expected Truncated=true for oversized result")
	}
	s, _ := p.Result.(string)
	if len(s) > toolPayloadFieldLimit+64 { // 允许截断标记的少量额外字节
		t.Fatalf("result not truncated: len=%d", len(s))
	}
}

func TestToolCallPayloadFromRecord_SpillStubKeepsPathWhenTruncated(t *testing.T) {
	row := map[string]any{"z": strings.Repeat("z", 3000)}
	sample := make([]map[string]any, 5)
	for i := range sample {
		sample[i] = row
	}
	rec := agent.ToolCallRecord{
		ToolCallID: "call_spill",
		ToolName:   "es_log_query",
		Result: &tool.QuerySpillStub{
			Spilled: true,
			Path:    "tmp/results/sess/1_es_log_query_1.jsonl",
			Count:   5,
			OK:      true,
			Sample:  sample,
		},
	}
	p := toolCallPayloadFromRecord(rec, "completed")
	if !p.Truncated {
		t.Fatal("expected Truncated=true for fat spill sample")
	}
	s, ok := p.Result.(string)
	if !ok {
		t.Fatalf("expected truncated result string, got %T", p.Result)
	}
	if !strings.Contains(s, "tmp/results/sess") {
		t.Fatalf("truncated result lost spill path: %s", s)
	}
}
