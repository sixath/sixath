package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func TestBuildSyntheticAskUserMessages_Fulfilled(t *testing.T) {
	pending := tool.PendingInputRequest{
		ToolCallID:       "call_1",
		RequestID:        "req_1",
		Token:            "tok_1",
		Field:            "ssh_password",
		Kind:             "password",
		Prompt:           "Enter password",
		ReasoningContent: "need password before ssh_exec",
	}
	msgs := BuildSyntheticAskUserMessages(pending, SyntheticAskUserOutcomeFulfilled)
	if len(msgs) != 2 {
		t.Fatalf("want assistant+tool, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" || msgs[1].Role != "tool" {
		t.Fatal("roles wrong")
	}
	if strings.Contains(msgs[1].Content, "hunter2") {
		t.Fatal("tool content must not contain secret")
	}
	rc, ok := msgs[0].Metadata[model.MetadataKeyReasoningContent].(string)
	if !ok {
		t.Fatalf("assistant metadata missing reasoning_content: %#v", msgs[0].Metadata)
	}
	if rc != "need password before ssh_exec" {
		t.Fatalf("reasoning_content: got %q", rc)
	}
	if _, ok := msgs[0].Metadata["tool_calls"]; !ok {
		t.Fatal("assistant metadata missing tool_calls")
	}
}

func TestBuildSyntheticAskUserMessages_EmptyReasoningStillPresent(t *testing.T) {
	pending := tool.PendingInputRequest{
		ToolCallID: "call_2",
		RequestID:  "req_2",
		Field:      "jaeger_query",
		Kind:       "text",
		Prompt:     "trace id?",
	}
	msgs := BuildSyntheticAskUserMessages(pending, SyntheticAskUserOutcomeFulfilled)
	rc, ok := msgs[0].Metadata[model.MetadataKeyReasoningContent].(string)
	if !ok {
		t.Fatalf("reasoning_content key must exist even when empty: %#v", msgs[0].Metadata)
	}
	if rc != "" {
		t.Fatalf("want empty reasoning, got %q", rc)
	}
}

func TestUserMessagePlaceholderForInput(t *testing.T) {
	got := UserMessagePlaceholderForInput("ssh_password")
	if got != "[input provided: ssh_password]" {
		t.Fatalf("%q", got)
	}
}

func TestApplyInputResponse_PasswordUsesFulfillmentStore(t *testing.T) {
	pendingStore := tool.NewInMemoryAskUserPendingStore()
	fulfillStore := tool.NewInMemoryAskUserFulfillmentStore()
	ctx := context.Background()
	_ = pendingStore.SavePending(ctx, "sess_1", tool.PendingInputRequest{
		RequestID: "req_1",
		Token:     "tok_1",
		SessionID: "sess_1",
		Field:     "ssh_password",
		Kind:      "password",
		Prompt:    "pwd",
	})
	pending, outcome, err := ApplyInputResponse(ctx, "sess_1", InputResponse{
		Token: "tok_1",
		Value: "secret",
	}, pendingStore, fulfillStore)
	if err != nil || outcome != SyntheticAskUserOutcomeFulfilled || pending.Field != "ssh_password" {
		t.Fatalf("apply: pending=%#v outcome=%v err=%v", pending, outcome, err)
	}
	v, err := fulfillStore.GetSecret(ctx, "sess_1", "ssh_password")
	if err != nil || v != "secret" {
		t.Fatalf("secret: %q err=%v", v, err)
	}
}

func TestInjectSyntheticBeforeLastUser(t *testing.T) {
	synthetic := []model.Message{{Role: "tool", Content: "{}"}}
	messages := []model.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
		{Role: "user", Content: "[input provided: x]"},
	}
	out := InjectSyntheticBeforeLastUser(messages, synthetic)
	if len(out) != 4 || out[2].Role != "tool" || out[3].Role != "user" {
		t.Fatalf("got %#v", out)
	}
}
