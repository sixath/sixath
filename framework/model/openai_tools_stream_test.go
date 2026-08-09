package model

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sixath/framework/tool"
)

// writeSSE writes each line as an SSE `data: <line>\n\n` frame, flushing after
// every frame to mimic OpenAI's token-by-token chat completions streaming.
func writeSSE(t *testing.T, w http.ResponseWriter, frames []string) {
	t.Helper()
	flusher, ok := w.(http.Flusher)
	if !ok {
		t.Fatalf("ResponseWriter does not support flushing")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	for _, f := range frames {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", f); err != nil {
			t.Fatalf("write SSE frame: %v", err)
		}
		flusher.Flush()
	}
}

// registerFakeTool registers a trivial no-op tool so ChatWithToolsStream does
// not error on an empty registry.
func registerFakeTool(t *testing.T, reg *tool.Registry, name string) {
	t.Helper()
	if err := reg.Register(tool.Tool{
		Name:        name,
		Description: "fake tool for streaming tests",
		Execute: func(context.Context, map[string]any) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("register fake tool %q: %v", name, err)
	}
}

// TestChatWithToolsStream_AccumulatesAcrossChunksWithEmptyFinishReason proves
// that intermediate chunks carrying finish_reason="" do not terminate the loop:
// the content pieces are accumulated across chunks and the final Generation
// carries the full text. On the old buggy code (which treated "" as terminal),
// the loop returned on the first empty-finish_reason chunk and produced empty text.
func TestChatWithToolsStream_AccumulatesAcrossChunksWithEmptyFinishReason(t *testing.T) {
	frames := []string{
		`{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":" "}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"world"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`[DONE]`,
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeSSE(t, w, frames)
	}))
	defer ts.Close()

	client := openAITestClient(ts)
	reg := tool.NewRegistry()
	registerFakeTool(t, reg, "fake_tool")

	textCh, genCh, err := client.ChatWithToolsStream(
		context.Background(),
		[]Message{{Role: "user", Content: "hi"}},
		reg,
	)
	if err != nil {
		t.Fatalf("ChatWithToolsStream error: %v", err)
	}

	var streamed strings.Builder
	for piece := range textCh {
		streamed.WriteString(piece)
	}
	if got := streamed.String(); got != "Hello world" {
		t.Fatalf("streamed text = %q, want %q", got, "Hello world")
	}

	gen, ok := <-genCh
	if !ok || gen == nil {
		t.Fatalf("expected a final Generation, got none")
	}
	if gen.Text != "Hello world" {
		t.Fatalf("final Generation.Text = %q, want %q", gen.Text, "Hello world")
	}
	step, ok := gen.Raw.(ToolStep)
	if !ok {
		t.Fatalf("expected ToolStep raw, got %#v", gen.Raw)
	}
	if step.Used {
		t.Fatalf("expected no tool use for a plain text stream, got %#v", step)
	}
}

// TestChatWithToolsStream_AssemblesToolCallAcrossChunks proves that tool_calls
// arriving in pieces across intermediate chunks (with finish_reason="") are
// assembled into one complete tool call, terminating only on
// finish_reason="tool_calls". On the old buggy code the first empty-finish_reason
// chunk returned early, so the tool call was never assembled.
func TestChatWithToolsStream_AssemblesToolCallAcrossChunks(t *testing.T) {
	frames := []string{
		// First chunk: id + name + partial args.
		`{"id":"c2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"fake_tool","arguments":"{\"vm"}}]}}]}`,
		// Second chunk: rest of the arguments (empty finish_reason).
		`{"id":"c2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"id\":36115}"}}]}}]}`,
		// Final chunk: terminal tool_calls finish reason.
		`{"id":"c2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSSE(t, w, frames)
	}))
	defer ts.Close()

	client := openAITestClient(ts)
	reg := tool.NewRegistry()
	registerFakeTool(t, reg, "fake_tool")

	textCh, genCh, err := client.ChatWithToolsStream(
		context.Background(),
		[]Message{{Role: "user", Content: "call the tool"}},
		reg,
	)
	if err != nil {
		t.Fatalf("ChatWithToolsStream error: %v", err)
	}

	// Drain (no content expected for a pure tool-call stream).
	for range textCh {
	}

	gen, ok := <-genCh
	if !ok || gen == nil {
		t.Fatalf("expected a final Generation, got none")
	}
	step, ok := gen.Raw.(ToolStep)
	if !ok {
		t.Fatalf("expected ToolStep raw, got %#v", gen.Raw)
	}
	if !step.Used {
		t.Fatalf("expected tool use, got %#v", step)
	}
	if len(step.ToolCalls) != 1 {
		t.Fatalf("expected exactly 1 assembled tool call, got %d: %#v", len(step.ToolCalls), step.ToolCalls)
	}
	call := step.ToolCalls[0]
	if call.ID != "call_abc" {
		t.Fatalf("tool call ID = %q, want %q", call.ID, "call_abc")
	}
	if call.Name != "fake_tool" {
		t.Fatalf("tool call Name = %q, want %q", call.Name, "fake_tool")
	}
	// Arguments assembled across chunks: {"vm" + id":36115} => {"vmid":36115}
	if call.RawArgumentsParseError != "" {
		t.Fatalf("unexpected argument parse error: %q (preview=%q)", call.RawArgumentsParseError, call.RawArgumentsPreview)
	}
	if got := call.Arguments["vmid"]; got != float64(36115) {
		t.Fatalf("assembled argument vmid = %#v, want 36115 (full args=%#v)", got, call.Arguments)
	}
}

// TestChatWithToolsStream_TerminatesOnEOFWithoutFinishReason locks down the
// EOF-only termination path: some gateways never emit a named finish_reason and
// rely solely on stream close to signal the end. The loop must build the final
// Generation from the accumulated content when it hits io.EOF (the DONE frame)
// without ever having seen a terminal finish_reason. This is the safety net that
// makes "falling through on empty finish_reason" safe.
func TestChatWithToolsStream_TerminatesOnEOFWithoutFinishReason(t *testing.T) {
	frames := []string{
		`{"id":"c3","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}`,
		`{"id":"c3","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":" "}}]}`,
		`{"id":"c3","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"world"}}]}`,
		// No terminal finish_reason chunk at all: stream just ends.
		`[DONE]`,
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSSE(t, w, frames)
	}))
	defer ts.Close()

	client := openAITestClient(ts)
	reg := tool.NewRegistry()
	registerFakeTool(t, reg, "fake_tool")

	textCh, genCh, err := client.ChatWithToolsStream(
		context.Background(),
		[]Message{{Role: "user", Content: "hi"}},
		reg,
	)
	if err != nil {
		t.Fatalf("ChatWithToolsStream error: %v", err)
	}

	var streamed strings.Builder
	for piece := range textCh {
		streamed.WriteString(piece)
	}
	if got := streamed.String(); got != "Hello world" {
		t.Fatalf("streamed text = %q, want %q", got, "Hello world")
	}

	gen, ok := <-genCh
	if !ok || gen == nil {
		t.Fatalf("expected a final Generation from the EOF path, got none")
	}
	if gen.Text != "Hello world" {
		t.Fatalf("final Generation.Text = %q, want %q", gen.Text, "Hello world")
	}
	step, ok := gen.Raw.(ToolStep)
	if !ok {
		t.Fatalf("expected ToolStep raw, got %#v", gen.Raw)
	}
	if step.Used {
		t.Fatalf("expected no tool use for a plain text stream, got %#v", step)
	}
}

// TestChatWithToolsStream_PropagatesRecvError 锁定：中途 Recv 失败时必须经 genCh
// 带回 Err，而不能空关闭 channel（否则 ReAct 只看到 "missing streamed generation"）。
func TestChatWithToolsStream_PropagatesRecvError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("ResponseWriter does not support flushing")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"id":"c4","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"partial"}}]}`)
		flusher.Flush()
		// 故意写非法帧，触发 go-openai Recv 解析错误（非 EOF）。
		_, _ = fmt.Fprintf(w, "data: {not-json\n\n")
		flusher.Flush()
	}))
	defer ts.Close()

	client := openAITestClient(ts)
	reg := tool.NewRegistry()
	registerFakeTool(t, reg, "fake_tool")

	textCh, genCh, err := client.ChatWithToolsStream(
		context.Background(),
		[]Message{{Role: "user", Content: "hi"}},
		reg,
	)
	if err != nil {
		t.Fatalf("ChatWithToolsStream error: %v", err)
	}

	for range textCh {
	}
	gen, ok := <-genCh
	if !ok || gen == nil {
		t.Fatalf("expected Generation carrying Err, got closed/nil channel")
	}
	if gen.Err == nil {
		t.Fatalf("expected gen.Err from mid-stream Recv failure, got %#v", gen)
	}
}

