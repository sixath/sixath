package model

import (
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestPatchStrictGateways_EmptyAssistantNoToolCallsGetsPlaceholder(t *testing.T) {
	msg := openai.ChatCompletionMessage{Role: "assistant", Content: ""}
	patchChatCompletionMessageForStrictGateways(&msg)
	if msg.Content == "" {
		t.Fatal("empty assistant content (no tool_calls) must be replaced with a placeholder")
	}
}

func TestPatchStrictGateways_WhitespaceOnlyUserGetsPlaceholder(t *testing.T) {
	msg := openai.ChatCompletionMessage{Role: "user", Content: "   "}
	patchChatCompletionMessageForStrictGateways(&msg)
	if len(msg.Content) == 0 {
		t.Fatal("whitespace-only content must remain non-empty")
	}
}

func TestPatchStrictGateways_NonEmptyContentUnchanged(t *testing.T) {
	msg := openai.ChatCompletionMessage{Role: "assistant", Content: "hello"}
	patchChatCompletionMessageForStrictGateways(&msg)
	if msg.Content != "hello" {
		t.Fatalf("non-empty content must be preserved, got %q", msg.Content)
	}
}

func TestPatchStrictGateways_AssistantWithToolCallsStillGetsSpace(t *testing.T) {
	msg := openai.ChatCompletionMessage{
		Role: "assistant", Content: "",
		ToolCalls: []openai.ToolCall{{ID: "c1", Type: openai.ToolTypeFunction}},
	}
	patchChatCompletionMessageForStrictGateways(&msg)
	if msg.Content != " " {
		t.Fatalf("assistant+toolcalls empty content should be single space, got %q", msg.Content)
	}
}
