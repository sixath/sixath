package model

import (
	"strings"
	"testing"
)

func tc(name string, args map[string]any) ToolCall {
	return ToolCall{Name: name, Arguments: args, ID: "call-" + name}
}

func assistantToolRound(calls ...ToolCall) Message {
	return Message{
		Role:     "assistant",
		Content:  " ",
		Metadata: map[string]any{"tool_calls": calls},
	}
}

func toolResult(name, callID, content string) Message {
	return Message{
		Role:    "tool",
		Content: content,
		Metadata: map[string]any{
			"tool_name":    name,
			"tool_call_id": callID,
		},
	}
}

func TestSnipCompactMessages_RemovesSupersededReadFileChain(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "read foo twice"},
		assistantToolRound(tc("read_file", map[string]any{"path": "src/foo.go"})),
		toolResult("read_file", "call-read_file", `{"tool":"read_file","result":"v1"}`),
		assistantToolRound(tc("read_file", map[string]any{"path": "src/foo.go"})),
		toolResult("read_file", "call-read_file", `{"tool":"read_file","result":"v2"}`),
		{Role: "user", Content: "done"},
	}
	out, removed := SnipCompactMessages(msgs)
	if removed != 2 {
		t.Fatalf("removed want 2, got %d", removed)
	}
	if len(out) != len(msgs)-2 {
		t.Fatalf("len want %d, got %d", len(msgs)-2, len(out))
	}
	if strings.Contains(out[2].Content, "v1") {
		t.Fatalf("expected first read_file tool result removed")
	}
}

func TestSnipCompactMessages_KeepsMixedChainWithWriteFile(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		assistantToolRound(
			tc("read_file", map[string]any{"path": "a.go"}),
			tc("write_file", map[string]any{"path": "a.go", "content": "x"}),
		),
		toolResult("read_file", "c1", `{}`),
		toolResult("write_file", "c2", `{}`),
		assistantToolRound(tc("read_file", map[string]any{"path": "a.go"})),
		toolResult("read_file", "c3", `{}`),
	}
	out, removed := SnipCompactMessages(msgs)
	if removed != 0 {
		t.Fatalf("mixed chain must not snip, removed=%d", removed)
	}
	if len(out) != len(msgs) {
		t.Fatalf("len unchanged want %d got %d", len(msgs), len(out))
	}
}

func TestSnipCompactMessages_WebSearchQueryDedup(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "s"},
		assistantToolRound(tc("web_search", map[string]any{"query": "Go 1.22 release"})),
		toolResult("web_search", "c1", `{}`),
		assistantToolRound(tc("web_search", map[string]any{"query": "  go   1.22   release  "})),
		toolResult("web_search", "c2", `{}`),
	}
	_, removed := SnipCompactMessages(msgs)
	if removed != 2 {
		t.Fatalf("removed want 2, got %d", removed)
	}
}

func TestSnipCompactMessages_SkipsProtectedRuntimeMessages(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys", Metadata: map[string]any{MetadataKeySixathOrigin: OriginL2Handoff}},
		assistantToolRound(tc("todo", map[string]any{})),
		toolResult("todo", "c1", `{}`),
	}
	out, removed := SnipCompactMessages(msgs)
	if removed != 0 || len(out) != len(msgs) {
		t.Fatalf("protected prefix should not affect snip: removed=%d len=%d", removed, len(out))
	}
}

func TestPrepareChatContext_EmitsSnipCompact(t *testing.T) {
	var kinds []string
	msgs := []Message{
		{Role: "system", Content: "sys"},
		assistantToolRound(tc("read_file", map[string]any{"path": "x"})),
		toolResult("read_file", "c1", `{}`),
		assistantToolRound(tc("read_file", map[string]any{"path": "x"})),
		toolResult("read_file", "c2", `{}`),
	}
	cfg := &CallConfig{
		SnipCompactEnabled: true,
		ContextTrace: func(kind string, detail map[string]any) {
			kinds = append(kinds, kind)
		},
	}
	out := PrepareChatContext(msgs, cfg)
	if len(out) >= len(msgs) {
		t.Fatalf("expected snip to remove messages")
	}
	found := false
	for _, k := range kinds {
		if k == "snip_compact" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected snip_compact trace, got %v", kinds)
	}
}

func TestPrepareChatContext_SnipDisabled(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		assistantToolRound(tc("read_file", map[string]any{"path": "x"})),
		toolResult("read_file", "c1", `{}`),
		assistantToolRound(tc("read_file", map[string]any{"path": "x"})),
		toolResult("read_file", "c2", `{}`),
	}
	out := PrepareChatContext(msgs, &CallConfig{SnipCompactEnabled: false})
	if len(out) != len(msgs) {
		t.Fatalf("snip disabled should keep all messages, got %d", len(out))
	}
}
