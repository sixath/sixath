package harness

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sixath/framework/model"
)

func TestRunTrace_ModelCallRecord_JSON(t *testing.T) {
	tr := &RunTrace{
		ModelCalls: []ModelCallRecord{
			{
				Step:         1,
				Mode:         "tools",
				Reasoning:    "thinking step",
				Answer:       "final answer",
				FinishReason: "stop",
				InputTokens:  10,
				OutputTokens: 5,
				Messages: []TracedMessage{
					{Role: "user", Content: "hello"},
				},
			},
		},
	}
	b, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	calls, ok := m["model_calls"].([]any)
	if !ok {
		t.Fatalf("expected model_calls array, got %T", m["model_calls"])
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 model_call, got %d", len(calls))
	}
	rec := calls[0].(map[string]any)
	if got := rec["reasoning"]; got != "thinking step" {
		t.Fatalf("expected reasoning=%q, got %v", "thinking step", got)
	}
	if got := rec["answer"]; got != "final answer" {
		t.Fatalf("expected answer=%q, got %v", "final answer", got)
	}
	if got := rec["finish_reason"]; got != "stop" {
		t.Fatalf("expected finish_reason=%q, got %v", "stop", got)
	}
}

func TestRunTrace_ModelCallRecord_EmptyOmit(t *testing.T) {
	tr := &RunTrace{}
	b, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	if strings.Contains(string(b), "model_calls") {
		t.Fatalf("expected model_calls omitted for empty slice, got: %s", string(b))
	}
}

func TestRunTrace_RecordModelCall_ExtractsReasoningAndAnswer(t *testing.T) {
	tr := &RunTrace{}
	messages := []model.Message{
		{Role: "user", Content: "hello"},
	}
	gen := &model.Generation{
		Text:         "ok",
		FinishReason: "tool_calls",
		TokenUsage:   &model.TokenUsage{InputTokens: 3, OutputTokens: 2},
		Raw:          model.ToolStep{ReasoningContent: "chain", Used: true},
	}
	tr.recordModelCall(messages, gen, "tools", 0)

	if len(tr.ModelCalls) != 1 {
		t.Fatalf("expected 1 model call, got %d", len(tr.ModelCalls))
	}
	rec := tr.ModelCalls[0]
	if rec.Reasoning != "chain" {
		t.Fatalf("expected reasoning=chain, got %q", rec.Reasoning)
	}
	if rec.Answer != "ok" {
		t.Fatalf("expected answer=ok, got %q", rec.Answer)
	}
	if rec.FinishReason != "tool_calls" {
		t.Fatalf("expected finish_reason=tool_calls, got %q", rec.FinishReason)
	}
	if rec.InputTokens != 3 {
		t.Fatalf("expected input_tokens=3, got %d", rec.InputTokens)
	}
	if rec.OutputTokens != 2 {
		t.Fatalf("expected output_tokens=2, got %d", rec.OutputTokens)
	}
	if rec.Step != 0 {
		t.Fatalf("expected step=0, got %d", rec.Step)
	}
	if rec.Mode != "tools" {
		t.Fatalf("expected mode=tools, got %q", rec.Mode)
	}
	if len(rec.Messages) != 1 {
		t.Fatalf("expected 1 traced message, got %d", len(rec.Messages))
	}
	if rec.Messages[0].Content != "hello" {
		t.Fatalf("expected message content=hello, got %q", rec.Messages[0].Content)
	}
}

func TestRunTrace_RecordModelCall_RedactsCredentials(t *testing.T) {
	tr := &RunTrace{}
	messages := []model.Message{
		{Role: "tool", Content: "password=supersecret"},
	}
	gen := &model.Generation{Text: "ok"}
	tr.recordModelCall(messages, gen, "tool", 0)

	if len(tr.ModelCalls) == 0 {
		t.Fatalf("expected at least 1 model call")
	}
	content := tr.ModelCalls[0].Messages[0].Content
	if strings.Contains(content, "supersecret") {
		t.Fatalf("expected credential redacted: %q", content)
	}
}

func TestRunTrace_RecordModelCall_TruncatesLongContent(t *testing.T) {
	tr := &RunTrace{}
	longContent := strings.Repeat("x", 10000)
	messages := []model.Message{
		{Role: "user", Content: longContent},
	}
	gen := &model.Generation{Text: "short"}
	tr.recordModelCall(messages, gen, "chat", 1)

	if len(tr.ModelCalls) == 0 {
		t.Fatalf("expected at least 1 model call")
	}
	got := tr.ModelCalls[0].Messages[0].Content
	// Should be truncated; allow a small tolerance for multi-byte safety
	if len(got) > 2048+8 {
		t.Fatalf("expected truncated content <= %d, got %d", 2048+8, len(got))
	}
}

func TestRunTrace_RecordModelCall_NilGen(t *testing.T) {
	tr := &RunTrace{}
	messages := []model.Message{
		{Role: "user", Content: "hi"},
	}
	tr.recordModelCall(messages, nil, "", 0)

	if len(tr.ModelCalls) != 1 {
		t.Fatalf("expected 1 record even with nil gen, got %d", len(tr.ModelCalls))
	}
	if tr.ModelCalls[0].Answer != "" || tr.ModelCalls[0].Reasoning != "" {
		t.Fatalf("expected empty answer/reasoning for nil gen: %+v", tr.ModelCalls[0])
	}
}
