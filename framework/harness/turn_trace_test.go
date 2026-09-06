package harness

import (
	"strings"
	"testing"
)

func TestBuildTurnTrace_UnwrapsToolCallBridge(t *testing.T) {
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		Step: 0, ToolCallID: "c1", ToolName: "execute_read",
		Arguments: map[string]any{"sql": "select 1"},
		Result:    map[string]any{"rows": []any{}},
	}}}
	out := BuildTurnTrace(TurnTraceMeta{SessionID: "s", AgentID: "a", RequestID: "r1"}, tr)
	if len(out.Calls) != 1 || out.Calls[0].ToolName != "execute_read" {
		t.Fatalf("%+v", out)
	}
	if out.RequestID != "r1" {
		t.Fatal(out.RequestID)
	}
}

func TestBuildTurnTrace_RedactsSecretKeysAndTruncates(t *testing.T) {
	big := strings.Repeat("x", 10_000)
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName:  "http",
		Arguments: map[string]any{"password": "secret", "q": "ok"},
		Result:    big,
	}}}
	out := BuildTurnTrace(TurnTraceMeta{SessionID: "s", AgentID: "a", RequestID: "r"}, tr)
	if out.Calls[0].Arguments["password"] != "[redacted]" {
		t.Fatalf("args: %#v", out.Calls[0].Arguments)
	}
	if len(out.Calls[0].ResultPreview) > 4096+32 {
		t.Fatalf("preview too long: %d", len(out.Calls[0].ResultPreview))
	}
}

func TestBuildTurnTrace_NilTrace(t *testing.T) {
	if BuildTurnTrace(TurnTraceMeta{}, nil) != nil {
		t.Fatal("expected nil")
	}
}
