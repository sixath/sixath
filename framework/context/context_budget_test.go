package context

import (
	"github.com/sixath/framework/model"
	"strings"
	"testing"
)

func TestCompressMessagesByRunesBudget_DropsOlderUserBlock(t *testing.T) {
	u1 := strings.Repeat("x", 80)
	u2 := strings.Repeat("y", 70)
	msgs := []model.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: u1},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: u2},
		{Role: "assistant", Content: "a2"},
	}
	out := CompressMessagesByRunesBudget(msgs, 120)
	if len(out) >= len(msgs) {
		t.Fatalf("expected fewer messages, got len=%d", len(out))
	}
	if out[0].Role != "system" || out[0].Content != "sys" {
		t.Fatalf("expected system preserved, got %#v", out[0])
	}
	if out[1].Role != "user" || !strings.Contains(out[1].Content, "上下文已压缩") {
		t.Fatalf("expected compression notice user message, got %#v", out[1])
	}
	if !strings.Contains(out[len(out)-2].Content, u2[:8]) {
		t.Fatalf("expected recent user block kept, last users: %#v", out[len(out)-2])
	}
}

func TestCompressMessagesByRunesBudget_NoOpUnderBudget(t *testing.T) {
	msgs := []model.Message{{Role: "user", Content: "hi"}}
	out := CompressMessagesByRunesBudget(msgs, 10_000)
	if len(out) != 1 || out[0].Content != "hi" {
		t.Fatalf("unexpected: %#v", out)
	}
}

func TestStripLeadingOrphanToolsAfterSystem(t *testing.T) {
	sys := model.Message{Role: "system", Content: "s"}
	tool := model.Message{Role: "tool", Content: `{"tool":"x"}`, Metadata: map[string]any{"tool_call_id": "c1"}}
	u := model.Message{Role: "user", Content: "hi"}
	out := stripLeadingOrphanToolsAfterSystem([]model.Message{sys, tool, u})
	if len(out) != 2 || out[1].Content != "hi" {
		t.Fatalf("expected system+user, got %#v", out)
	}
}

func TestStripLeadingOrphanToolsAfterCompressionNote(t *testing.T) {
	sys := model.Message{Role: "system", Content: "s"}
	note := model.Message{Role: "user", Content: "[上下文已压缩：已省略较早的 1 条消息；以下为保留的最近对话。]"}
	tool := model.Message{Role: "tool", Content: `{}`, Metadata: map[string]any{"tool_call_id": "c1"}}
	u := model.Message{Role: "user", Content: "real"}
	out := stripLeadingOrphanToolsAfterSystem([]model.Message{sys, note, tool, u})
	// 压缩说明后紧跟孤立 tool 时一并去掉说明与 tool，保留后续合法 user。
	if len(out) != 2 || out[1].Content != "real" {
		t.Fatalf("expected system+real user, got len=%d %#v", len(out), out)
	}
}

func TestCompressMessagesByRunesBudget_Disabled(t *testing.T) {
	msgs := []model.Message{{Role: "user", Content: strings.Repeat("z", 100)}}
	out := CompressMessagesByRunesBudget(msgs, 0)
	if len(out) != 1 {
		t.Fatalf("expected unchanged")
	}
}

func TestCompressMessagesByRunesBudget_DropsLeadingToolRound(t *testing.T) {
	msgs := []model.Message{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: " ", Metadata: map[string]any{"tool_calls": []model.ToolCall{{
			ID: "c1", Name: "t", Arguments: map[string]any{},
		}}}},
		{Role: "tool", Content: strings.Repeat("o", 200), Metadata: map[string]any{"tool_call_id": "c1"}},
		{Role: "assistant", Content: "final"},
	}
	before := totalMessageRunes(msgs)
	out := CompressMessagesByRunesBudget(msgs, 80)
	if totalMessageRunes(out) >= before {
		t.Fatalf("expected lower total runes after compression, before=%d after=%d", before, totalMessageRunes(out))
	}
	foundFinal := false
	for _, m := range out {
		if m.Role == "assistant" && m.Content == "final" {
			foundFinal = true
		}
	}
	if !foundFinal {
		t.Fatalf("expected final assistant kept: %#v", out)
	}
}
