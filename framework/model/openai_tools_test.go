package model

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	openai "github.com/sashabaranov/go-openai"

	"github.com/sixath/framework/tool"
)

func TestOpenAIClient_ChatWithTools_ReturnsToolRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "",
					ToolCalls: []openai.ToolCall{{
						ID:   "call_1",
						Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{
							Name:      "calculator_add",
							Arguments: `{"a":1.5,"b":2.5}`,
						},
					}},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode response error: %v", err)
		}
	}))
	defer ts.Close()

	client := openAITestClient(ts)
	reg := tool.NewRegistry()
	if err := tool.RegisterCalculatorTool(reg); err != nil {
		t.Fatalf("register calculator tool error: %v", err)
	}

	gen, err := client.ChatWithTools(context.Background(), []Message{{Role: "user", Content: "1.5 + 2.5"}}, reg)
	if err != nil {
		t.Fatalf("ChatWithTools error: %v", err)
	}
	step, ok := gen.Raw.(ToolStep)
	if !ok {
		t.Fatalf("expected ToolStep raw, got %#v", gen.Raw)
	}
	if !step.Used || step.ToolCallID != "call_1" || step.ToolName != "calculator_add" {
		t.Fatalf("unexpected tool step: %#v", step)
	}
	if step.Arguments["a"] != float64(1.5) || step.Arguments["b"] != float64(2.5) {
		t.Fatalf("unexpected tool arguments: %#v", step.Arguments)
	}
}

func TestOpenAIClient_ChatWithTools_ReturnsMultipleToolRequests(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role: openai.ChatMessageRoleAssistant,
					ToolCalls: []openai.ToolCall{{
						ID:   "call_1",
						Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{
							Name:      "calculator_add",
							Arguments: `{"a":1,"b":2}`,
						},
					}, {
						ID:   "call_2",
						Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{
							Name:      "calculator_add",
							Arguments: `{"a":10,"b":5}`,
						},
					}},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := openAITestClient(ts)
	reg := tool.NewRegistry()
	if err := tool.RegisterCalculatorTool(reg); err != nil {
		t.Fatalf("register calculator tool error: %v", err)
	}

	gen, err := client.ChatWithTools(context.Background(), []Message{{Role: "user", Content: "two sums"}}, reg)
	if err != nil {
		t.Fatalf("ChatWithTools error: %v", err)
	}
	step := gen.Raw.(ToolStep)
	if !step.Used || len(step.ToolCalls) != 2 {
		t.Fatalf("expected two tool calls, got %#v", step)
	}
	if step.ToolCalls[0].ID != "call_1" || step.ToolCalls[1].ID != "call_2" {
		t.Fatalf("unexpected call ids: %#v", step.ToolCalls)
	}
	if step.ToolCalls[0].Arguments["a"] != float64(1) || step.ToolCalls[1].Arguments["b"] != float64(5) {
		t.Fatalf("unexpected arguments: %#v", step.ToolCalls)
	}
}

func TestOpenAIClient_ChatWithTools_InvalidArgumentsStillReturnsToolCall(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role: openai.ChatMessageRoleAssistant,
					ToolCalls: []openai.ToolCall{{
						ID:   "call_bad",
						Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{
							Name:      "calculator_add",
							Arguments: `{"a":`,
						},
					}},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := openAITestClient(ts)
	reg := tool.NewRegistry()
	if err := tool.RegisterCalculatorTool(reg); err != nil {
		t.Fatalf("register calculator tool error: %v", err)
	}

	gen, err := client.ChatWithTools(context.Background(), []Message{{Role: "user", Content: "bad args"}}, reg)
	if err != nil {
		t.Fatalf("ChatWithTools should not fail on invalid arguments json: %v", err)
	}
	step := gen.Raw.(ToolStep)
	if !step.Used || len(step.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %#v", step)
	}
	call := step.ToolCalls[0]
	if call.RawArgumentsParseError == "" {
		t.Fatalf("expected parse error marker, got %#v", call)
	}
	if len(call.Arguments) != 0 {
		t.Fatalf("expected empty arguments on parse failure, got %#v", call.Arguments)
	}
}

func TestOpenAIClient_ChatWithTools_DoesNotExecuteTool(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role: openai.ChatMessageRoleAssistant,
					ToolCalls: []openai.ToolCall{{
						ID:   "call_1",
						Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{
							Name:      "failing_tool",
							Arguments: `{}`,
						},
					}},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := openAITestClient(ts)
	executed := false
	reg := tool.NewRegistry()
	_ = reg.Register(tool.Tool{
		Name: "failing_tool",
		Execute: func(context.Context, map[string]any) (any, error) {
			executed = true
			return nil, nil
		},
	})

	gen, err := client.ChatWithTools(context.Background(), []Message{{Role: "user", Content: "test"}}, reg)
	if err != nil {
		t.Fatalf("ChatWithTools error: %v", err)
	}
	if executed {
		t.Fatalf("model layer executed tool; agent layer should own execution")
	}
	step := gen.Raw.(ToolStep)
	if !step.Used || step.ToolName != "failing_tool" {
		t.Fatalf("expected failing_tool request, got %#v", step)
	}
}

func TestOpenAIClient_ChatWithTools_SendsNativeToolMessages(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []map[string]any `json:"messages"`
		}
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(r.Body)
		if err := json.Unmarshal(buf.Bytes(), &body); err != nil {
			t.Fatalf("decode request: %v body=%s", err, buf.String())
		}
		if len(body.Messages) != 3 {
			t.Fatalf("expected 3 messages, got %#v", body.Messages)
		}
		assistant := body.Messages[1]
		if assistant["role"] != "assistant" {
			t.Fatalf("expected assistant tool request, got %#v", assistant)
		}
		calls, ok := assistant["tool_calls"].([]any)
		if !ok || len(calls) != 1 {
			t.Fatalf("expected assistant tool_calls, got %#v", assistant)
		}
		call := calls[0].(map[string]any)
		if call["id"] != "call_1" || call["type"] != "function" {
			t.Fatalf("unexpected tool call: %#v", call)
		}
		toolMsg := body.Messages[2]
		if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_1" {
			t.Fatalf("expected tool message with call id, got %#v", toolMsg)
		}

		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "done",
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := openAITestClient(ts)
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	gen, err := client.ChatWithTools(context.Background(), []Message{{
		Role: "user", Content: "sum",
	}, {
		Role: "assistant",
		Metadata: map[string]any{"tool_calls": []ToolCall{{
			ID:        "call_1",
			Name:      "calculator_add",
			Arguments: map[string]any{"a": float64(1), "b": float64(2)},
		}}},
	}, {
		Role:    "tool",
		Content: `{"result":3}`,
		Metadata: map[string]any{
			"tool_call_id": "call_1",
		},
	}}, reg)
	if err != nil {
		t.Fatalf("ChatWithTools error: %v", err)
	}
	if gen.Text != "done" {
		t.Fatalf("unexpected response: %#v", gen)
	}
}

func TestOpenAIChatMessage_PreservesReasoningContent(t *testing.T) {
	msg, err := openAIChatMessage(Message{
		Role:    "assistant",
		Content: " ",
		Metadata: map[string]any{
			MetadataKeyReasoningContent: "chain-of-thought hidden",
			"tool_calls": []ToolCall{{
				ID:        "call_1",
				Name:      "memory_search",
				Arguments: map[string]any{"query": "test"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.ReasoningContent != "chain-of-thought hidden" {
		t.Fatalf("expected reasoning_content preserved, got %q", msg.ReasoningContent)
	}
}

func TestOpenAIChatMessage_ToolCallsAlwaysSetReasoningContent(t *testing.T) {
	// Missing metadata key: still set empty string so the field is present on the struct
	// (DeepSeek requires reasoning_content whenever tool_calls is set).
	msg, err := openAIChatMessage(Message{
		Role:    "assistant",
		Content: "",
		Metadata: map[string]any{
			"tool_calls": []ToolCall{{
				ID:        "call_1",
				Name:      "ask_user",
				Arguments: map[string]any{"prompt": "x"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	if msg.ReasoningContent != "" {
		t.Fatalf("expected empty reasoning_content when key absent, got %q", msg.ReasoningContent)
	}

	msg2, err := openAIChatMessage(Message{
		Role:    "assistant",
		Content: "",
		Metadata: map[string]any{
			MetadataKeyReasoningContent: "",
			"tool_calls": []ToolCall{{
				ID:        "call_2",
				Name:      "ask_user",
				Arguments: map[string]any{"prompt": "y"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg2.ReasoningContent != "" {
		t.Fatalf("expected empty string preserved, got %q", msg2.ReasoningContent)
	}
}

func TestOpenAIChatMessage_AssistantToolCallsEmptyContentPatched(t *testing.T) {
	msg, err := openAIChatMessage(Message{
		Role:    "assistant",
		Content: "",
		Metadata: map[string]any{
			"tool_calls": []ToolCall{{
				ID:        "call_1",
				Name:      "ssh_exec",
				Arguments: map[string]any{"host": "h"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != " " {
		t.Fatalf("expected single-space placeholder, got %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
}

func TestOpenAIChatMessage_ToolEmptyContentPatched(t *testing.T) {
	msg, err := openAIChatMessage(Message{
		Role:     "tool",
		Content:  "",
		Metadata: map[string]any{"tool_call_id": "call_1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(msg.Content) == "" {
		t.Fatalf("expected non-empty tool content, got %q", msg.Content)
	}
}

func TestOpenAIChatMessage_ContentTruncated(t *testing.T) {
	long := strings.Repeat("a", openAICompatMaxMessageRunes+100)
	msg, err := openAIChatMessage(Message{Role: "user", Content: long})
	if err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(msg.Content) > openAICompatMaxMessageRunes {
		t.Fatalf("content still too long: %d", utf8.RuneCountInString(msg.Content))
	}
	if !strings.Contains(msg.Content, "[truncated") {
		t.Fatalf("expected truncation marker, tail: %q", tailSnippet(msg.Content, 120))
	}
}

func tailSnippet(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

func openAITestClient(ts *httptest.Server) *OpenAIClient {
	cfg := openai.DefaultConfig("test")
	cfg.BaseURL = ts.URL
	cfg.HTTPClient = ts.Client()
	return &OpenAIClient{
		apiKey: "test",
		client: openai.NewClientWithConfig(cfg),
		model:  "fake-model",
	}
}

func TestParseToolArguments_RepairTruncatedJSONObject(t *testing.T) {
	got, repaired, parseErr := parseToolArguments(`{"a":1,"b":2`)
	if !repaired {
		t.Fatalf("expected repaired=true")
	}
	if parseErr != "" {
		t.Fatalf("expected empty parseErr, got %q", parseErr)
	}
	if got["a"] != float64(1) || got["b"] != float64(2) {
		t.Fatalf("unexpected parsed args: %#v", got)
	}
}

func TestParseToolArguments_RejectInvalidJSON(t *testing.T) {
	got, repaired, parseErr := parseToolArguments(`{"a":`)
	if parseErr == "" {
		t.Fatalf("expected parse error for invalid json")
	}
	if repaired {
		t.Fatalf("expected repaired=false for invalid json")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty args for invalid json, got %#v", got)
	}
}
