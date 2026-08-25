package agent

import (
	"strings"
	"testing"
)

func TestEvaluateInboundCompletenessGate_requiresReferences(t *testing.T) {
	records := []ToolCallRecord{{
		ToolName: "rca_read",
		Result:   map[string]any{"ok": true, "file": "helper.go", "content": "1|x"},
	}}
	got := EvaluateInboundCompletenessGate(records, "下面给出存档迁移的整体流程。")
	if got.Allow {
		t.Fatal("overall flow without references must not allow")
	}
	if got.Action != "inject" || !strings.Contains(got.Prompt, "未扫入边") || !strings.Contains(got.Prompt, "references") {
		t.Fatalf("got %#v", got)
	}
}

func TestEvaluateInboundCompletenessGate_referencesAllows(t *testing.T) {
	records := []ToolCallRecord{
		{ToolName: "rca_read", Result: map[string]any{"ok": true, "file": "h.go", "content": "1|x"}},
		{ToolName: "rca_symbol", Arguments: map[string]any{"action": "references"}, Result: map[string]any{
			"ok": true, "action": "references", "inbound_empty": true, "callers": []map[string]any{},
		}},
	}
	if got := EvaluateInboundCompletenessGate(records, "入边为空，以下为整体流程。"); !got.Allow {
		t.Fatalf("inbound_empty scan must allow: %#v", got)
	}
}

func TestEvaluateInboundCompletenessGate_noClaimAllows(t *testing.T) {
	records := []ToolCallRecord{{ToolName: "rca_read", Result: map[string]any{"ok": true, "file": "h.go", "content": "1|x"}}}
	if got := EvaluateInboundCompletenessGate(records, "errcode=1105 时不写入映射。"); !got.Allow {
		t.Fatalf("no overall-flow claim should allow: %#v", got)
	}
}

func TestEvaluateInboundCompletenessGate_ackAllows(t *testing.T) {
	records := []ToolCallRecord{{ToolName: "rca_grep", Result: map[string]any{"ok": true, "matches": []any{}}}}
	if got := EvaluateInboundCompletenessGate(records, "未扫入边，不能给出整体流程。"); !got.Allow {
		t.Fatalf("ack should allow: %#v", got)
	}
}
