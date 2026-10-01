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

func TestBuildTurnTrace_RedactsCredentialsInValues(t *testing.T) {
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName:  "ssh_exec",
		Arguments: map[string]any{"command": "sshpass -p 's3cret' ssh root@h"},
	}}}
	out := BuildTurnTrace(TurnTraceMeta{}, tr)
	cmd, _ := out.Calls[0].Arguments["command"].(string)
	if strings.Contains(cmd, "s3cret") {
		t.Fatalf("command not redacted: %q", cmd)
	}
}

func TestBuildTurnTrace_PreviewRedactsDecodedValues(t *testing.T) {
	type res struct {
		URL     string `json:"url"`
		User    string `json:"user"`
		Command string `json:"command"`
		Stdout  string `json:"stdout"`
	}
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "ssh_exec",
		Result: res{
			URL:     "http://10.0.0.1:9200",
			User:    "admin@corp",
			Command: `sshpass -p "pw123" ssh h`,
			Stdout:  `password="pw123"`,
		},
	}}}
	p := BuildTurnTrace(TurnTraceMeta{}, tr).Calls[0].ResultPreview
	if strings.Contains(p, "pw123") {
		t.Fatalf("quoted secret leaked: %s", p)
	}
	if !strings.Contains(p, `"user":"admin@corp"`) || !strings.Contains(p, `"url":"http://10.0.0.1:9200"`) {
		t.Fatalf("fields damaged: %s", p)
	}
}

func TestBuildTurnTrace_PreviewKeepsLargeIntWhenRedacted(t *testing.T) {
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "x",
		Result:   map[string]any{"id": int64(1234567890123456789), "stdout": "password=x"},
	}}}
	p := BuildTurnTrace(TurnTraceMeta{}, tr).Calls[0].ResultPreview
	if !strings.Contains(p, "1234567890123456789") || strings.Contains(p, "password=x") {
		t.Fatalf("large int lost or secret leaked: %s", p)
	}
}

func TestBuildTurnTrace_RedactsResultPreviewAndError(t *testing.T) {
	type sshResult struct {
		Command string `json:"command"`
		Stdout  string `json:"stdout"`
	}
	results := []any{
		"password=s3cret",
		map[string]any{"stdout": "login password=s3cret ok"},
		sshResult{Command: "sshpass -p s3cret ssh h", Stdout: "ok"},
	}
	for i, res := range results {
		tr := &RunTrace{ToolCalls: []ToolCallRecord{{
			ToolName: "ssh_exec",
			Result:   res,
			Error:    "auth failed: token=s3cret",
		}}}
		out := BuildTurnTrace(TurnTraceMeta{}, tr)
		if p := out.Calls[0].ResultPreview; strings.Contains(p, "s3cret") {
			t.Fatalf("case %d: preview not redacted: %q", i, p)
		}
		if e := out.Calls[0].Error; strings.Contains(e, "s3cret") {
			t.Fatalf("case %d: error not redacted: %q", i, e)
		}
	}
}
