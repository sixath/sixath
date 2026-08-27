package chat

import (
	"testing"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
)

func TestFinalTextFromDone(t *testing.T) {
	prev := []model.Message{{Role: "assistant", Content: "我来查一下"}}
	if got := FinalTextFromDone("该服务从未参与", prev); got != "该服务从未参与" {
		t.Fatalf("prefer done text → %q", got)
	}
	if got := FinalTextFromDone("", prev); got != "我来查一下" {
		t.Fatalf("fallback last assistant → %q", got)
	}
}

func TestLastAssistantText(t *testing.T) {
	if got := LastAssistantText(nil); got != "" {
		t.Fatalf("empty list → %q", got)
	}
	if got := LastAssistantText([]model.Message{}); got != "" {
		t.Fatalf("empty slice → %q", got)
	}
	msgs := []model.Message{
		{Role: "assistant", Content: "first"},
		{Role: "user", Content: "ok"},
		{Role: "assistant", Content: "   "},
		{Role: "assistant", Content: "last"},
		{Role: "user", Content: "thanks"},
	}
	if got := LastAssistantText(msgs); got != "last" {
		t.Fatalf("last non-empty assistant → %q", got)
	}
}

func TestToolHitsFromTrace(t *testing.T) {
	if got := ToolHitsFromTrace(nil); got != nil {
		t.Fatalf("nil → %#v", got)
	}

	hits := ToolHitsFromTrace(&agent.RunTrace{ToolCalls: []agent.ToolCallRecord{{
		ToolName: "es_log_query",
		Result: map[string]any{
			"hit_status":    "empty",
			"queried_index": "vm-manager-*",
		},
	}}})
	if len(hits) != 1 {
		t.Fatalf("len=%d", len(hits))
	}
	if hits[0].ToolName != "es_log_query" || hits[0].HitStatus != "empty" || hits[0].QueriedIndex != "vm-manager-*" {
		t.Fatalf("%+v", hits[0])
	}

	missing := ToolHitsFromTrace(&agent.RunTrace{ToolCalls: []agent.ToolCallRecord{{
		ToolName: "es_log_query",
		Result:   map[string]any{"hits": []any{}},
	}}})
	if len(missing) != 1 || missing[0].HitStatus != "" {
		t.Fatalf("missing hit_status → %+v", missing)
	}
}
