package service

import (
	"strings"
	"testing"

	"github.com/sixath/framework/agent"
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
