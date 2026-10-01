package service

import (
	"encoding/json"
	"strings"
	"testing"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/tool"
	tooldata "github.com/sixath/framework/tool/data"
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

func TestToolCallPayloadFromRecord_RedactsCredentials(t *testing.T) {
	rec := agent.ToolCallRecord{
		ToolCallID: "call_r",
		ToolName:   "terminal",
		Arguments: map[string]any{
			"command": "curl -u ftpuser:hunter2 ftp://10.0.0.1/a",
			"headers": map[string]any{"Authorization": "Bearer abc"},
		},
		Result: "connected with password=hunter2",
	}
	p := toolCallPayloadFromRecord(rec, "completed")
	args, _ := p.Arguments.(map[string]any)
	if cmd, _ := args["command"].(string); strings.Contains(cmd, "hunter2") {
		t.Fatalf("command not redacted: %q", cmd)
	}
	h, _ := args["headers"].(map[string]any)
	if h["Authorization"] != "***" {
		t.Fatalf("header not redacted: %#v", h)
	}
	if s, _ := p.Result.(string); strings.Contains(s, "hunter2") {
		t.Fatalf("string result not redacted: %q", s)
	}
	if cmd := rec.Arguments["command"].(string); strings.Contains(cmd, "***") || !strings.Contains(cmd, "hunter2") {
		t.Fatalf("record arguments must not be mutated: %q", cmd)
	}
}

func payloadResultJSON(t *testing.T, p *ToolCallPayload) string {
	t.Helper()
	b, err := json.Marshal(p.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	return string(b)
}

func TestToolCallPayloadFromRecord_RedactsMapResult(t *testing.T) {
	rec := agent.ToolCallRecord{
		ToolCallID: "call_m",
		ToolName:   "terminal",
		Result: map[string]any{
			"stdout": "password=x",
			"rows":   []map[string]any{{"dsn": "mysql://root:s3cret@db/app"}},
		},
	}
	p := toolCallPayloadFromRecord(rec, "completed")
	s := payloadResultJSON(t, p)
	if strings.Contains(s, "password=x") || strings.Contains(s, "s3cret") {
		t.Fatalf("map result not redacted: %s", s)
	}
}

func TestToolCallPayloadFromRecord_RedactsStructResult(t *testing.T) {
	rec := agent.ToolCallRecord{
		ToolCallID: "call_s",
		ToolName:   "ssh_exec",
		Result:     tool.SSHExecResult{OK: true, Host: "h", Command: "sshpass -p s3cret ssh h", Stdout: "ok"},
	}
	p := toolCallPayloadFromRecord(rec, "completed")
	s := payloadResultJSON(t, p)
	if strings.Contains(s, "s3cret") {
		t.Fatalf("struct result not redacted: %s", s)
	}
	if !strings.Contains(s, `"host":"h"`) {
		t.Fatalf("struct result lost fields: %s", s)
	}
}

func TestToolCallPayloadFromRecord_RedactsError(t *testing.T) {
	rec := agent.ToolCallRecord{ToolCallID: "call_e", ToolName: "terminal", Error: "login failed: password=s3cret"}
	p := toolCallPayloadFromRecord(rec, "completed")
	if strings.Contains(p.Error, "s3cret") {
		t.Fatalf("error not redacted: %q", p.Error)
	}
}

func TestToolCallPayloadFromRecord_NilArgumentsStayNil(t *testing.T) {
	p := toolCallPayloadFromRecord(agent.ToolCallRecord{ToolCallID: "call_n", ToolName: "x"}, "started")
	if p.Arguments != nil {
		t.Fatalf("expected nil arguments, got %#v", p.Arguments)
	}
	if p.Result != nil {
		t.Fatalf("expected nil result, got %#v", p.Result)
	}
}

func TestToolCallPayloadFromRecord_SpillStubWithSecretKeepsPath(t *testing.T) {
	row := map[string]any{"msg": "token=s3cret " + strings.Repeat("z", 3000)}
	sample := make([]map[string]any, 5)
	for i := range sample {
		sample[i] = row
	}
	rec := agent.ToolCallRecord{
		ToolCallID: "call_spill_secret",
		ToolName:   "es_log_query",
		Result: &tool.QuerySpillStub{
			Spilled: true,
			Path:    "tmp/results/sess/2_es_log_query_1.jsonl",
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
	if !strings.Contains(s, "tmp/results/sess/2_es_log_query_1.jsonl") {
		t.Fatalf("truncated result lost spill path: %s", s)
	}
	if n := strings.Count(s, "s3cret"); n > 0 {
		t.Fatalf("spill sample not redacted: %d occurrences", n)
	}
}

func payloadResultMap(t *testing.T, p *ToolCallPayload) map[string]any {
	t.Helper()
	m, ok := p.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T: %#v", p.Result, p.Result)
	}
	return m
}

func TestToolCallPayloadFromRecord_PendingExecuteWriteKeepsToken(t *testing.T) {
	results := []any{
		&tooldata.ExecuteWritePendingResponse{Status: "pending", Token: "t-123", DSL: "UPDATE u SET password='s3cret'", ExpiresIn: 60},
		map[string]any{"status": "pending", "token": "t-123", "dsl": "UPDATE u SET password='s3cret'", "expires_in": 60},
	}
	for i, res := range results {
		p := toolCallPayloadFromRecord(agent.ToolCallRecord{ToolCallID: "call_w", ToolName: "execute_write", Result: res}, "completed")
		m := payloadResultMap(t, p)
		if m["token"] != "t-123" {
			t.Fatalf("case %d: pending token changed: %#v", i, m["token"])
		}
		if strings.Contains(payloadResultJSON(t, p), "s3cret") {
			t.Fatalf("case %d: dsl not redacted: %#v", i, m)
		}
	}
}

func TestToolCallPayloadFromRecord_PendingAskUserKeepsToken(t *testing.T) {
	res := map[string]any{
		"status":     "pending",
		"request_id": "req-1",
		"token":      "tok_abc",
		"kind":       "credential",
		"field":      "password",
		"prompt":     "enter password",
		"options":    []string{"a", "b"},
		"required":   true,
		"expires_in": 300,
	}
	p := toolCallPayloadFromRecord(agent.ToolCallRecord{ToolCallID: "call_a", ToolName: "ask_user", Result: res}, "completed")
	if p.Result.(map[string]any)["token"] != "tok_abc" {
		t.Fatalf("pending ask_user token changed: %#v", p.Result)
	}
}

func TestToolCallPayloadFromRecord_NonPendingTokenMasked(t *testing.T) {
	res := map[string]any{"status": "done", "token": "tok_abc"}
	p := toolCallPayloadFromRecord(agent.ToolCallRecord{ToolCallID: "call_d", ToolName: "x", Result: res}, "completed")
	if payloadResultMap(t, p)["token"] != "***" {
		t.Fatalf("non-pending token should be masked: %#v", p.Result)
	}
}

func TestToolCallPayloadFromRecord_URLAndUserFieldsIntact(t *testing.T) {
	type esResult struct {
		URL  string `json:"url"`
		User string `json:"user"`
	}
	results := []any{
		map[string]any{"url": "http://10.0.0.1:9200", "user": "admin@corp"},
		esResult{URL: "http://10.0.0.1:9200", User: "admin@corp"},
	}
	for i, res := range results {
		p := toolCallPayloadFromRecord(agent.ToolCallRecord{ToolCallID: "call_u", ToolName: "es", Result: res}, "completed")
		s := payloadResultJSON(t, p)
		if !strings.Contains(s, `"user":"admin@corp"`) || !strings.Contains(s, `"url":"http://10.0.0.1:9200"`) {
			t.Fatalf("case %d: fields damaged: %s", i, s)
		}
	}
}

func TestToolCallPayloadFromRecord_QuotedSecretsInStructMasked(t *testing.T) {
	p := toolCallPayloadFromRecord(agent.ToolCallRecord{
		ToolCallID: "call_q",
		ToolName:   "ssh_exec",
		Result: tool.SSHExecResult{
			Host:    "h",
			Command: `sshpass -p "pw123" ssh h`,
			Stdout:  `config: password="pw123"`,
		},
	}, "completed")
	m := payloadResultMap(t, p)
	if s := payloadResultJSON(t, p); strings.Contains(s, "pw123") {
		t.Fatalf("quoted secret leaked: %s", s)
	}
	if m["host"] != "h" {
		t.Fatalf("host lost: %#v", m)
	}
}

func TestToolCallPayloadFromRecord_RedactedResultKeepsLargeInt(t *testing.T) {
	res := map[string]any{"id": int64(1234567890123456789), "stdout": "password=x"}
	p := toolCallPayloadFromRecord(agent.ToolCallRecord{ToolCallID: "call_i", ToolName: "x", Result: res}, "completed")
	s := payloadResultJSON(t, p)
	if !strings.Contains(s, "1234567890123456789") || strings.Contains(s, "password=x") {
		t.Fatalf("large int lost or secret leaked: %s", s)
	}
}

func TestToolCallPayloadFromRecord_SecretKeyBoolStaysMap(t *testing.T) {
	type sshOpts struct {
		Host        string `json:"host"`
		UsePassword bool   `json:"use_password"`
	}
	p := toolCallPayloadFromRecord(agent.ToolCallRecord{
		ToolCallID: "call_b",
		ToolName:   "ssh_exec",
		Result:     sshOpts{Host: "h", UsePassword: true},
	}, "completed")
	m := payloadResultMap(t, p)
	if m["host"] != "h" {
		t.Fatalf("host lost: %#v", m)
	}
}
