package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

type fakeOpenAIClient struct {
	toolSteps        []model.ToolStep
	plainReplies     []string
	finalReply       string
	err              error
	chatCalls        int
	toolCalls        int
	lastToolMessages []model.Message
}

func (f *fakeOpenAIClient) nextPlainReply() string {
	if len(f.plainReplies) > 0 {
		s := f.plainReplies[0]
		f.plainReplies = f.plainReplies[1:]
		return s
	}
	return f.finalReply
}

func (f *fakeOpenAIClient) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	_ = ctx
	_ = prompt
	_ = opts
	return &model.Generation{Text: f.finalReply}, nil
}

func (f *fakeOpenAIClient) Chat(ctx context.Context, msgs []model.Message, opts ...model.Option) (*model.Generation, error) {
	_ = ctx
	cfg := model.ApplyOptions(opts...)
	msgs = model.PrepareChatContext(msgs, cfg)
	_ = msgs
	f.chatCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &model.Generation{Text: f.finalReply, Raw: model.ToolStep{Used: false}}, nil
}

func (f *fakeOpenAIClient) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	_ = ctx
	_ = opts
	out := make([]model.Embedding, len(texts))
	return out, nil
}

func (f *fakeOpenAIClient) ChatWithTools(ctx context.Context, messages []model.Message, reg *tool.Registry, opts ...model.Option) (*model.Generation, error) {
	_ = ctx
	_ = reg
	cfg := model.ApplyOptions(opts...)
	messages = model.PrepareChatContext(messages, cfg)
	f.toolCalls++
	f.lastToolMessages = append([]model.Message(nil), messages...)
	if f.err != nil {
		return nil, f.err
	}
	text := f.nextPlainReply()
	if len(f.toolSteps) > 0 {
		step := f.toolSteps[0]
		f.toolSteps = f.toolSteps[1:]
		return &model.Generation{Text: text, Raw: step}, nil
	}
	return &model.Generation{
		Text: text,
		Raw:  model.ToolStep{Used: false},
	}, nil
}

func TestReActAgent_ModelErrorReturnsRunErrorWithTrace(t *testing.T) {
	expectErr := errors.New("model exploded")
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{err: expectErr}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	react := NewReActAgent(fake, mem, reg)
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	})
	if !errors.Is(err, expectErr) {
		t.Fatalf("expected wrapped model error, got %v", err)
	}
	var runErr *RunError
	if !errors.As(err, &runErr) {
		t.Fatalf("expected RunError, got %T %[1]v", err)
	}
	if runErr.Trace == nil || len(runErr.Trace.Errors) != 1 {
		t.Fatalf("expected trace with error, got %#v", runErr.Trace)
	}
}

func TestReActAgent_EmptyIdleAfterToolsDoesNotInject(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{
			{
				Used:      true,
				ToolName:  "calculator_add",
				Arguments: map[string]any{"a": float64(1), "b": float64(1)},
			},
			{Used: false},
		},
		plainReplies: []string{""},
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	react := NewReActAgent(fake, mem, reg, WithReActMaxSteps(5))
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "暂停vmid=232451的实例cgvmagent进程"}},
	})
	if err != nil {
		t.Fatalf("unexpected err=%v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if fake.toolCalls != 2 {
		t.Fatalf("toolCalls=%d want 2 (no idle inject round)", fake.toolCalls)
	}
	trace, _ := resp.Metadata["trace"].(*RunTrace)
	if trace != nil && trace.EmptyIdleNudges != 0 {
		t.Fatalf("EmptyIdleNudges=%d want 0", trace.EmptyIdleNudges)
	}
	for _, m := range fake.lastToolMessages {
		if m.Role == "user" && strings.Contains(m.Content, "没有写出给用户看的正文") {
			t.Fatalf("must not inject empty-idle prompt: %#v", m)
		}
	}
}

func TestReActAgent_EmptyIdleWithoutToolsFinishes(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{finalReply: "\n\n"}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	react := NewReActAgent(fake, mem, reg)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "你好"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || resp.Text != "\n\n" {
		t.Fatalf("empty idle without tools must not inject, got %#v", resp)
	}
	if fake.toolCalls != 1 {
		t.Fatalf("toolCalls=%d want 1", fake.toolCalls)
	}
}

func TestReActAgent_toolResultsDoNotInsertCodeWorkset(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{
			{
				Used:      true,
				ToolName:  "calculator_add",
				Arguments: map[string]any{"a": float64(1), "b": float64(1)},
			},
			{Used: false},
		},
		plainReplies: []string{"done"},
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	react := NewReActAgent(fake, mem, reg, WithReActMaxSteps(5))
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "1+1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range fake.lastToolMessages {
		if strings.Contains(m.Content, "[code_workset]") {
			t.Fatalf("workset card must not appear: %#v", m)
		}
	}
}

func TestReActAgent_MaxStepsForcedSummary(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:      true,
			ToolName:  "calculator_add",
			Arguments: map[string]any{"a": float64(1), "b": float64(1)},
		}},
		finalReply: "summary table",
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	react := NewReActAgent(fake, mem, reg, WithReActMaxSteps(1))
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "summarize all hosts"}},
	})
	if err != nil {
		t.Fatalf("expected forced summary success, got err=%v", err)
	}
	if resp == nil || resp.Text != "summary table" {
		t.Fatalf("expected summary table, got %#v", resp)
	}
}

func TestReActAgent_MaxStepsReturnsRunErrorWithTrace(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:      true,
			ToolName:  "calculator_add",
			Arguments: map[string]any{"a": float64(1), "b": float64(1)},
		}},
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	react := NewReActAgent(fake, mem, reg, WithReActMaxSteps(1))
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "loop"}},
	})
	if err == nil {
		t.Fatalf("expected max steps error")
	}
	var runErr *RunError
	if !errors.As(err, &runErr) {
		t.Fatalf("expected RunError, got %T %[1]v", err)
	}
	if runErr.Trace == nil || len(runErr.Trace.ToolCalls) != 1 || len(runErr.Trace.Errors) != 1 {
		t.Fatalf("expected trace with tool call and max step error, got %#v", runErr.Trace)
	}
}

func TestReActAgent_IncludesSystemPrompt(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{finalReply: "ok"}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	react := NewReActAgent(fake, mem, reg, WithReActSystemPrompt("answer as a database assistant"))
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fake.lastToolMessages) == 0 {
		t.Fatalf("expected messages to be sent")
	}
	first := fake.lastToolMessages[0]
	if first.Role != "system" || first.Content != "answer as a database assistant" {
		t.Fatalf("expected system prompt first, got %#v", fake.lastToolMessages)
	}
}

func TestReActAgent_NoToolCallReturnsDirectly(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{finalReply: "direct answer"}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	react := NewReActAgent(fake, mem, reg)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if resp == nil || resp.Text != "direct answer" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if fake.toolCalls != 1 {
		t.Fatalf("expected one ChatWithTools call, got %d", fake.toolCalls)
	}
	if fake.chatCalls != 0 {
		t.Fatalf("expected no plain Chat calls, got %d", fake.chatCalls)
	}
}

func TestReActAgent_ExecutesToolAndRecordsTrace(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:      true,
			ToolName:  "calculator_add",
			Arguments: map[string]any{"a": float64(1), "b": float64(3)},
		}},
		finalReply: "the result is 4",
	}
	reg := tool.NewRegistry()
	if err := tool.RegisterCalculatorTool(reg); err != nil {
		t.Fatalf("register calculator tool error: %v", err)
	}

	react := NewReActAgent(fake, mem, reg)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "1+3=?"}},
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if resp == nil || resp.Text != "the result is 4" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	trace, ok := resp.Metadata["trace"].(*RunTrace)
	if !ok {
		t.Fatalf("expected trace metadata, got %#v", resp.Metadata)
	}
	if len(trace.ToolCalls) != 1 {
		t.Fatalf("expected one tool call in trace, got %#v", trace.ToolCalls)
	}
	call := trace.ToolCalls[0]
	if call.ToolName != "calculator_add" || call.Result != float64(4) || call.Error != "" {
		t.Fatalf("unexpected tool call trace: %#v", call)
	}
}

func TestReActAgent_ToolSuccessHookRunsOncePerSuccessfulTool(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:      true,
			ToolName:  "calculator_add",
			Arguments: map[string]any{"a": float64(1), "b": float64(3)},
		}},
		finalReply: "the result is 4",
	}
	reg := tool.NewRegistry()
	if err := tool.RegisterCalculatorTool(reg); err != nil {
		t.Fatalf("register calculator tool error: %v", err)
	}

	var hookCalls int
	var sawSession string
	req := &Request{
		Messages: []model.Message{{Role: "user", Content: "1+3=?"}},
		Metadata: map[string]any{"session_id": "sess-hook-1"},
	}
	react := NewReActAgent(fake, mem, reg, WithReActToolSuccessHook(func(ctx context.Context, r *Request, rec ToolCallRecord) {
		_ = ctx
		hookCalls++
		if r != req {
			t.Errorf("hook request pointer mismatch")
		}
		if rec.ToolName != "calculator_add" || rec.Error != "" || !rec.Allowed {
			t.Errorf("unexpected record in hook: %#v", rec)
		}
		if v, _ := r.Metadata["session_id"].(string); v != "" {
			sawSession = v
		}
	}))
	resp, err := react.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	if hookCalls != 1 {
		t.Fatalf("expected hook once, got %d", hookCalls)
	}
	if sawSession != "sess-hook-1" {
		t.Fatalf("metadata session_id: got %q", sawSession)
	}
}

func TestReActAgent_ExecutesMultipleToolCallsAndReturnsToolMessages(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used: true,
			ToolCalls: []model.ToolCall{{
				ID:        "call_1",
				Name:      "calculator_add",
				Arguments: map[string]any{"a": float64(1), "b": float64(3)},
			}, {
				ID:        "call_2",
				Name:      "calculator_add",
				Arguments: map[string]any{"a": float64(10), "b": float64(5)},
			}},
		}},
		finalReply: "both done",
	}
	reg := tool.NewRegistry()
	if err := tool.RegisterCalculatorTool(reg); err != nil {
		t.Fatalf("register calculator tool error: %v", err)
	}

	react := NewReActAgent(fake, mem, reg)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "run both"}},
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	trace := resp.Metadata["trace"].(*RunTrace)
	if len(trace.ToolCalls) != 2 {
		t.Fatalf("expected two tool calls, got %#v", trace.ToolCalls)
	}
	if trace.ToolCalls[0].Result != float64(4) || trace.ToolCalls[1].Result != float64(15) {
		t.Fatalf("unexpected tool results: %#v", trace.ToolCalls)
	}

	var toolMessages []model.Message
	for _, msg := range fake.lastToolMessages {
		if msg.Role == "tool" {
			toolMessages = append(toolMessages, msg)
		}
	}
	if len(toolMessages) != 2 {
		t.Fatalf("expected two tool messages in next model call, got %#v", fake.lastToolMessages)
	}
	if toolMessages[0].Metadata["tool_call_id"] != "call_1" || toolMessages[1].Metadata["tool_call_id"] != "call_2" {
		t.Fatalf("tool messages lost call ids: %#v", toolMessages)
	}
}

func TestReActAgent_MultipleToolCallsDeniedSecondKeepsTrace(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	scriptExecuted := false
	reg := tool.NewRegistry()
	if err := tool.RegisterCalculatorTool(reg); err != nil {
		t.Fatalf("register calculator tool error: %v", err)
	}
	if err := reg.Register(tool.Tool{
		Name: "execute_skill_script",
		Execute: func(context.Context, map[string]any) (any, error) {
			scriptExecuted = true
			return "ran", nil
		},
	}); err != nil {
		t.Fatalf("register script tool error: %v", err)
	}
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used: true,
			ToolCalls: []model.ToolCall{{
				ID:        "call_ok",
				Name:      "calculator_add",
				Arguments: map[string]any{"a": float64(1), "b": float64(2)},
			}, {
				ID:        "call_denied",
				Name:      "execute_skill_script",
				Arguments: map[string]any{"name": "x", "path": "scripts/run.sh"},
			}},
		}},
	}

	react := NewReActAgent(fake, mem, reg, WithReActPermissionPolicy(DenyTools("execute_skill_script")))
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "run tools"}},
	})
	if err == nil || !errors.Is(err, ErrToolPermissionDenied) {
		t.Fatalf("expected permission denied error, got %v", err)
	}
	var runErr *RunError
	if !errors.As(err, &runErr) {
		t.Fatalf("expected RunError with trace, got %T %[1]v", err)
	}
	if scriptExecuted {
		t.Fatalf("denied script tool executed")
	}
	if len(runErr.Trace.ToolCalls) != 2 {
		t.Fatalf("expected trace for allowed and denied calls, got %#v", runErr.Trace.ToolCalls)
	}
	if runErr.Trace.ToolCalls[0].Result != float64(3) || runErr.Trace.ToolCalls[1].Allowed {
		t.Fatalf("unexpected trace records: %#v", runErr.Trace.ToolCalls)
	}
}

func TestReActAgent_DeniesToolBeforeExecution(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	executed := false
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name: "execute_skill_script",
		Execute: func(context.Context, map[string]any) (any, error) {
			executed = true
			return "ran", nil
		},
	}); err != nil {
		t.Fatalf("register tool error: %v", err)
	}
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:      true,
			ToolName:  "execute_skill_script",
			Arguments: map[string]any{"name": "x", "path": "scripts/run.sh"},
		}},
	}

	react := NewReActAgent(fake, mem, reg, WithReActPermissionPolicy(DenyTools("execute_skill_script")))
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "run script"}},
	})
	if err == nil || !errors.Is(err, ErrToolPermissionDenied) {
		t.Fatalf("expected permission denied error, got resp=%#v err=%v", resp, err)
	}
	if executed {
		t.Fatalf("tool executed despite permission denial")
	}
}

type fakeStreamingToolClient struct {
	fakeOpenAIClient
	streamText string
	finalGen   *model.Generation
}

func (f *fakeStreamingToolClient) ChatWithToolsStream(ctx context.Context, messages []model.Message, reg *tool.Registry, opts ...model.Option) (<-chan string, <-chan *model.Generation, error) {
	cfg := model.ApplyOptions(opts...)
	_ = model.PrepareChatContext(messages, cfg)
	_ = reg
	ch := make(chan string)
	genCh := make(chan *model.Generation, 1)
	go func() {
		defer close(ch)
		defer close(genCh)
		for _, r := range f.streamText {
			select {
			case ch <- string(r):
			case <-ctx.Done():
				return
			}
		}
		if f.finalGen != nil {
			genCh <- f.finalGen
			return
		}
		genCh <- &model.Generation{Text: f.streamText, Raw: model.ToolStep{Used: false}}
	}()
	return ch, genCh, nil
}

func (f *fakeStreamingToolClient) ChatStream(ctx context.Context, messages []model.Message, opts ...model.Option) (<-chan string, error) {
	cfg := model.ApplyOptions(opts...)
	_ = model.PrepareChatContext(messages, cfg)
	ch := make(chan string)
	go func() {
		defer close(ch)
		for _, r := range f.streamText {
			select {
			case ch <- string(r):
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

type fakeMissingGenerationStreamClient struct {
	fakeOpenAIClient
}

func (f *fakeMissingGenerationStreamClient) ChatWithToolsStream(ctx context.Context, messages []model.Message, reg *tool.Registry, opts ...model.Option) (<-chan string, <-chan *model.Generation, error) {
	_ = f
	_ = ctx
	cfg := model.ApplyOptions(opts...)
	_ = model.PrepareChatContext(messages, cfg)
	_ = reg
	textCh := make(chan string)
	genCh := make(chan *model.Generation)
	close(textCh)
	close(genCh)
	return textCh, genCh, nil
}

type fakeBlockedGenerationStreamClient struct {
	fakeOpenAIClient
}

func (f *fakeBlockedGenerationStreamClient) ChatWithToolsStream(ctx context.Context, messages []model.Message, reg *tool.Registry, opts ...model.Option) (<-chan string, <-chan *model.Generation, error) {
	_ = f
	_ = ctx
	cfg := model.ApplyOptions(opts...)
	_ = model.PrepareChatContext(messages, cfg)
	_ = reg
	textCh := make(chan string)
	genCh := make(chan *model.Generation)
	close(textCh)
	return textCh, genCh, nil
}

func TestReActAgent_RunEvents_PlainStreamDeltaAndDone(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeStreamingToolClient{streamText: "plain stream"}

	react := NewReActAgent(fake, mem, nil)
	ch, err := react.RunEvents(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("RunEvents error: %v", err)
	}

	var got strings.Builder
	var types []StreamEventType
	var doneTrace *RunTrace
	var doneText string
	for event := range ch {
		types = append(types, event.Type)
		if event.Type == StreamEventDelta {
			got.WriteString(event.Text)
		}
		if event.Type == StreamEventDone {
			doneTrace = event.Trace
			doneText = event.Text
		}
	}
	if got.String() != "plain stream" {
		t.Fatalf("got streamed text %q, want %q", got.String(), "plain stream")
	}
	if doneText != "plain stream" {
		t.Fatalf("done Text %q, want gen.Text %q", doneText, "plain stream")
	}
	if !containsStreamEvent(types, StreamEventDelta) || !containsStreamEvent(types, StreamEventDone) {
		t.Fatalf("expected delta and done events, got %#v", types)
	}
	if doneTrace == nil {
		t.Fatalf("expected done event trace")
	}
}

func TestReActAgent_RunEvents_StreamedToolCall(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeStreamingToolClient{
		fakeOpenAIClient: fakeOpenAIClient{finalReply: "the result is 4"},
		finalGen: &model.Generation{
			Raw: model.ToolStep{
				Used: true,
				ToolCalls: []model.ToolCall{{
					ID:        "call_1",
					Name:      "calculator_add",
					Arguments: map[string]any{"a": float64(1), "b": float64(3)},
				}},
			},
		},
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	react := NewReActAgent(fake, mem, reg)
	ch, err := react.RunEvents(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "1+3=?"}},
	})
	if err != nil {
		t.Fatalf("RunEvents error: %v", err)
	}

	var got strings.Builder
	var types []StreamEventType
	var completed *ToolCallRecord
	var doneText string
	for event := range ch {
		types = append(types, event.Type)
		if event.Type == StreamEventDelta {
			got.WriteString(event.Text)
		}
		if event.Type == StreamEventDone {
			doneText = event.Text
		}
		if event.Type == StreamEventToolCompleted {
			completed = event.ToolCall
		}
	}
	if got.String() != "the result is 4" {
		t.Fatalf("got streamed text %q, want %q", got.String(), "the result is 4")
	}
	if doneText != "the result is 4" {
		t.Fatalf("idle done Text %q, want gen.Text %q", doneText, "the result is 4")
	}
	for _, want := range []StreamEventType{StreamEventToolStarted, StreamEventToolCompleted, StreamEventDelta, StreamEventDone} {
		if !containsStreamEvent(types, want) {
			t.Fatalf("expected stream event %q in %#v", want, types)
		}
	}
	if completed == nil || completed.ToolName != "calculator_add" || completed.Result != float64(4) {
		t.Fatalf("unexpected completed tool event: %#v", completed)
	}
	if fake.chatCalls != 1 {
		t.Fatalf("expected final Chat call after tool execution, got %d", fake.chatCalls)
	}
}

func TestReActAgent_RunEvents_PermissionDenied(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	executed := false
	fake := &fakeStreamingToolClient{
		finalGen: &model.Generation{
			Raw: model.ToolStep{
				Used: true,
				ToolCalls: []model.ToolCall{{
					ID:        "call_denied",
					Name:      "execute_skill_script",
					Arguments: map[string]any{"name": "x", "path": "scripts/run.sh"},
				}},
			},
		},
	}
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name: "execute_skill_script",
		Execute: func(context.Context, map[string]any) (any, error) {
			executed = true
			return "ran", nil
		},
	}); err != nil {
		t.Fatalf("register tool error: %v", err)
	}

	react := NewReActAgent(fake, mem, reg, WithReActPermissionPolicy(DenyTools("execute_skill_script")))
	ch, err := react.RunEvents(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "run script"}},
	})
	if err != nil {
		t.Fatalf("RunEvents error: %v", err)
	}

	var types []StreamEventType
	var denied *ToolCallRecord
	var errorTrace *RunTrace
	for event := range ch {
		types = append(types, event.Type)
		if event.Type == StreamEventPermissionDenied {
			denied = event.ToolCall
		}
		if event.Type == StreamEventError {
			errorTrace = event.Trace
		}
	}
	if executed {
		t.Fatalf("denied streamed tool was executed")
	}
	if !containsStreamEvent(types, StreamEventPermissionDenied) || !containsStreamEvent(types, StreamEventError) {
		t.Fatalf("expected permission_denied and error events, got %#v", types)
	}
	if denied == nil || denied.ToolName != "execute_skill_script" || denied.Allowed {
		t.Fatalf("unexpected denied tool event: %#v", denied)
	}
	if errorTrace == nil || len(errorTrace.Errors) != 1 || len(errorTrace.ToolCalls) != 1 {
		t.Fatalf("expected error event with trace, got %#v", errorTrace)
	}
}

func TestReActAgent_RunEvents_NonStreamingToolModelEmitsLifecycleOnce(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{finalReply: "direct answer"}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	bus := events.NewBus()
	var seen []events.Kind
	bus.Subscribe(false, func(ctx context.Context, e events.Event) {
		_ = ctx
		seen = append(seen, e.Kind)
	})

	react := NewReActAgent(fake, mem, reg, WithReActEventBus(bus))
	ch, err := react.RunEvents(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("RunEvents error: %v", err)
	}
	var got strings.Builder
	var types []StreamEventType
	var doneText string
	for event := range ch {
		types = append(types, event.Type)
		if event.Type == StreamEventDelta {
			got.WriteString(event.Text)
		}
		if event.Type == StreamEventDone {
			doneText = event.Text
		}
	}
	if got.String() != "direct answer" {
		t.Fatalf("got streamed text %q, want %q", got.String(), "direct answer")
	}
	if doneText != "direct answer" {
		t.Fatalf("idle done Text %q, want gen.Text %q", doneText, "direct answer")
	}
	if !containsStreamEvent(types, StreamEventDelta) || !containsStreamEvent(types, StreamEventDone) {
		t.Fatalf("expected delta and done events, got %#v", types)
	}
	if countEvent(seen, events.RunStarted) != 1 || countEvent(seen, events.RunCompleted) != 1 {
		t.Fatalf("expected single lifecycle event pair, got %#v", seen)
	}
}

func TestReActAgent_RunEvents_MissingStreamGenerationEmitsError(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeMissingGenerationStreamClient{}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	react := NewReActAgent(fake, mem, reg)
	ch, err := react.RunEvents(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("RunEvents error: %v", err)
	}

	var sawError bool
	var errorTrace *RunTrace
	for event := range ch {
		if event.Type == StreamEventError {
			sawError = true
			errorTrace = event.Trace
			if !strings.Contains(event.Error, "missing streamed generation") {
				t.Fatalf("unexpected error event: %#v", event)
			}
		}
	}
	if !sawError {
		t.Fatalf("expected error event when stream generation is missing")
	}
	if errorTrace == nil || len(errorTrace.Errors) != 1 {
		t.Fatalf("expected error event with trace, got %#v", errorTrace)
	}
}

func TestReActAgent_RunEvents_CanceledWhileWaitingForStreamGenerationEmitsError(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeBlockedGenerationStreamClient{}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	ctx, cancel := context.WithCancel(context.Background())

	react := NewReActAgent(fake, mem, reg)
	ch, err := react.RunEvents(ctx, &Request{
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("RunEvents error: %v", err)
	}
	cancel()

	deadline := time.After(time.Second)
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				t.Fatalf("stream closed before error event")
			}
			if event.Type == StreamEventError {
				if event.Error != context.Canceled.Error() {
					t.Fatalf("unexpected error event: %#v", event)
				}
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for context cancellation error event")
		}
	}
}

func TestReActAgent_RunStream_WithToolCallingStreamingModel(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeStreamingToolClient{
		fakeOpenAIClient: fakeOpenAIClient{finalReply: "fallback"},
		streamText:       "streaming answer",
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	react := NewReActAgent(fake, mem, reg)
	ch, err := react.RunStream(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("RunStream error: %v", err)
	}
	var got strings.Builder
	for s := range ch {
		got.WriteString(s)
	}
	if got.String() != "streaming answer" {
		t.Errorf("got streamed text %q, want %q", got.String(), "streaming answer")
	}
}

func TestReActAgent_RunStream_ExecutesStreamedToolCall(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeStreamingToolClient{
		fakeOpenAIClient: fakeOpenAIClient{finalReply: "the result is 4"},
		finalGen: &model.Generation{
			Raw: model.ToolStep{
				Used: true,
				ToolCalls: []model.ToolCall{{
					ID:        "call_1",
					Name:      "calculator_add",
					Arguments: map[string]any{"a": float64(1), "b": float64(3)},
				}},
			},
		},
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	bus := events.NewBus()
	var seen []events.Kind
	bus.Subscribe(false, func(ctx context.Context, e events.Event) {
		_ = ctx
		seen = append(seen, e.Kind)
	})

	react := NewReActAgent(fake, mem, reg, WithReActEventBus(bus))
	ch, err := react.RunStream(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "1+3=?"}},
	})
	if err != nil {
		t.Fatalf("RunStream error: %v", err)
	}
	var got strings.Builder
	for s := range ch {
		got.WriteString(s)
	}
	if got.String() != "the result is 4" {
		t.Fatalf("got streamed text %q, want %q", got.String(), "the result is 4")
	}
	if fake.chatCalls != 1 {
		t.Fatalf("expected final Chat call after tool execution, got %d", fake.chatCalls)
	}
	if !containsEvent(seen, events.ToolCompleted) {
		t.Fatalf("expected ToolCompleted event, got %#v", seen)
	}
}

func TestReActAgent_RunStream_DeniesStreamedToolCallWithEvents(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	executed := false
	fake := &fakeStreamingToolClient{
		finalGen: &model.Generation{
			Raw: model.ToolStep{
				Used: true,
				ToolCalls: []model.ToolCall{{
					ID:        "call_denied",
					Name:      "execute_skill_script",
					Arguments: map[string]any{"name": "x", "path": "scripts/run.sh"},
				}},
			},
		},
	}
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name: "execute_skill_script",
		Execute: func(context.Context, map[string]any) (any, error) {
			executed = true
			return "ran", nil
		},
	}); err != nil {
		t.Fatalf("register tool error: %v", err)
	}
	bus := events.NewBus()
	var seen []events.Kind
	bus.Subscribe(false, func(ctx context.Context, e events.Event) {
		_ = ctx
		seen = append(seen, e.Kind)
	})

	react := NewReActAgent(fake, mem, reg, WithReActEventBus(bus), WithReActPermissionPolicy(DenyTools("execute_skill_script")))
	ch, err := react.RunStream(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "run script"}},
	})
	if err != nil {
		t.Fatalf("RunStream error: %v", err)
	}
	for range ch {
	}
	if executed {
		t.Fatalf("denied streamed tool was executed")
	}
	if !containsEvent(seen, events.PermissionDenied) {
		t.Fatalf("expected PermissionDenied event, got %#v", seen)
	}
	if !containsEvent(seen, events.RunError) {
		t.Fatalf("expected RunError event, got %#v", seen)
	}
}

func sshExecTestRegistry(t *testing.T) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:        "ssh_exec",
		Description: "stub",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(context.Context, map[string]any) (any, error) {
			return nil, errors.New("ssh: denied")
		},
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

// TestDesignSection6_4_EventBusThreeSshExecFailures 对齐设计稿 §6.4：连续 3 次失败 ssh_exec 后事件总线应出现 agent.tool_guardrail.warn。
func TestDesignSection6_4_EventBusThreeSshExecFailures(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	reg := sshExecTestRegistry(t)
	args := map[string]any{"host": "h", "command": "ls"}
	steps := make([]model.ToolStep, 3)
	for i := range steps {
		steps[i] = model.ToolStep{Used: true, ToolName: "ssh_exec", Arguments: cloneArgs(args)}
	}
	fake := &fakeOpenAIClient{toolSteps: steps, finalReply: "ok"}
	bus := events.NewBus()
	var warns int
	bus.Subscribe(false, func(ctx context.Context, e events.Event) {
		_ = ctx
		if e.Kind == events.ToolGuardrailWarn {
			warns++
		}
	})
	gcfg := &ToolGuardrailsConfig{Enabled: true, HardHalt: false}
	react := NewReActAgent(fake, mem, reg, WithReActEventBus(bus), WithReActToolGuardrails(gcfg), WithReActMaxSteps(10))
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "x"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if warns < 1 {
		t.Fatalf("expected at least one %s, got %d", events.ToolGuardrailWarn, warns)
	}
}

// TestDesignSection6_4_FifthFailureHardHalt 对齐设计稿 §6.4：硬停开启时第 5 次同参失败应 RunError 且 trace.guardrail_halt。
func TestDesignSection6_4_FifthFailureHardHalt(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	reg := sshExecTestRegistry(t)
	args := map[string]any{"host": "h", "command": "ls"}
	steps := make([]model.ToolStep, 5)
	for i := range steps {
		steps[i] = model.ToolStep{Used: true, ToolName: "ssh_exec", Arguments: cloneArgs(args)}
	}
	fake := &fakeOpenAIClient{toolSteps: steps, finalReply: "nope"}
	gcfg := &ToolGuardrailsConfig{
		Enabled: true, HardHalt: true,
		SameArgsFailureWarn: 2, SameArgsFailureHalt: 5,
		SameToolFailureWarn: 100, SameToolFailureHalt: 0,
	}
	react := NewReActAgent(fake, mem, reg, WithReActToolGuardrails(gcfg), WithReActMaxSteps(10))
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "x"}},
	})
	if err == nil || !errors.Is(err, ErrToolGuardrailHalt) {
		t.Fatalf("want ErrToolGuardrailHalt on 5th same-args failure, got %v", err)
	}
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Trace == nil || !runErr.Trace.GuardrailHalt {
		t.Fatalf("expected trace guardrail_halt, got %#v", err)
	}
	if len(runErr.Trace.ToolCalls) != 5 {
		t.Fatalf("want 5 tool records before halt, got %d", len(runErr.Trace.ToolCalls))
	}
}

// TestReActAgent_RunEvents_Streaming_TwoSshExecBatch_GuardrailBeforePlain 覆盖 C4：同轮 tools_stream 内两条失败 ssh_exec 后、plain_after_tools 之前 R1 已基于完整 trace.ToolCalls 评估（事件早于 RunCompleted）。
func TestReActAgent_RunEvents_Streaming_TwoSshExecBatch_GuardrailBeforePlain(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	reg := sshExecTestRegistry(t)
	args := map[string]any{"host": "h1", "command": "ls"}
	fake := &fakeStreamingToolClient{
		fakeOpenAIClient: fakeOpenAIClient{finalReply: "done"},
		finalGen: &model.Generation{
			Raw: model.ToolStep{
				Used: true,
				ToolCalls: []model.ToolCall{
					{ID: "c1", Name: "ssh_exec", Arguments: cloneArgs(args)},
					{ID: "c2", Name: "ssh_exec", Arguments: cloneArgs(args)},
				},
			},
		},
	}
	bus := events.NewBus()
	var kinds []events.Kind
	bus.Subscribe(false, func(ctx context.Context, e events.Event) {
		_ = ctx
		kinds = append(kinds, e.Kind)
	})
	gcfg := &ToolGuardrailsConfig{Enabled: true, HardHalt: false}
	react := NewReActAgent(fake, mem, reg, WithReActEventBus(bus), WithReActToolGuardrails(gcfg), WithReActMaxSteps(10))
	ch, err := react.RunEvents(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	var warnIx, completedIx int
	for i, k := range kinds {
		if k == events.ToolGuardrailWarn && warnIx == 0 {
			warnIx = i + 1
		}
		if k == events.RunCompleted && completedIx == 0 {
			completedIx = i + 1
		}
	}
	if warnIx == 0 {
		t.Fatalf("expected %s in bus kinds: %#v", events.ToolGuardrailWarn, kinds)
	}
	if completedIx == 0 || warnIx >= completedIx {
		t.Fatalf("expected guardrail warn before RunCompleted, kinds=%#v warnIx=%d completedIx=%d", kinds, warnIx, completedIx)
	}
}

type countingGuardrailEvaluator struct {
	inner GuardrailEvaluator
	n     int
}

func (c *countingGuardrailEvaluator) Evaluate(h []ToolCallRecord, rounds int, emit func(events.Kind, map[string]any)) GuardrailDecision {
	c.n++
	return c.inner.Evaluate(h, rounds, emit)
}

func TestWithReActGuardrailEvaluatorOverridesDefault(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	reg := sshExecTestRegistry(t)
	args := map[string]any{"host": "h", "command": "ls"}
	fake := &fakeOpenAIClient{
		toolSteps:  []model.ToolStep{{Used: true, ToolName: "ssh_exec", Arguments: cloneArgs(args)}},
		finalReply: "done",
	}
	cfg := &ToolGuardrailsConfig{Enabled: true, HardHalt: false}
	spy := &countingGuardrailEvaluator{inner: NewGuardrailEvaluator(cfg)}
	react := NewReActAgent(fake, mem, reg,
		WithReActToolGuardrails(cfg),
		WithReActGuardrailEvaluator(spy),
		WithReActMaxSteps(10),
	)
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spy.n < 1 {
		t.Fatalf("expected custom GuardrailEvaluator invoked, got n=%d", spy.n)
	}
}

// TestReActAgent_Run_MetadataTraceJSONWithContextOps 对齐产品 §3.5 / §6.5：Run 结束后 trace 可 JSON 序列化且含 context_ops（含 invocation 明细）。
func TestReActAgent_Run_MetadataTraceJSONWithContextOps(t *testing.T) {
	mem := memory.NewBufferMemory(100)
	fake := &fakeOpenAIClient{finalReply: "ok"}
	var msgs []model.Message
	for i := 0; i < 25; i++ {
		msgs = append(msgs, model.Message{Role: "user", Content: strings.Repeat("x", 500)})
	}
	react := NewReActAgent(fake, mem, nil, WithReActMaxContextRunes(3000), WithReActMaxSteps(3))
	resp, err := react.Run(context.Background(), &Request{Messages: msgs})
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := resp.Metadata["trace"].(*RunTrace)
	if !ok || tr == nil {
		t.Fatalf("expected *RunTrace in metadata, got %#v", resp.Metadata["trace"])
	}
	if tr.ContextOps == nil || len(tr.ContextOps.Invocations) < 1 {
		t.Fatalf("expected context_ops.invocations, got %#v", tr.ContextOps)
	}
	if tr.ContextOps.L0DroppedMessages < 1 {
		t.Fatalf("expected L0 drop under tight budget, got l0_dropped=%d", tr.ContextOps.L0DroppedMessages)
	}
	if tr.ContextOps.Invocations[0].L0DroppedMessages < 1 {
		t.Fatalf("expected per-invocation l0_dropped, got %#v", tr.ContextOps.Invocations[0])
	}
	if _, err := json.Marshal(resp.Metadata["trace"]); err != nil {
		t.Fatalf("trace json: %v", err)
	}
}

func TestReActAgent_ToolGuardrailWarnsOnRepeatedSoftFailures(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:        "always_fail",
		Description: "always fails",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(context.Context, map[string]any) (any, error) {
			return nil, errors.New("boom")
		},
	}); err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus()
	var guardWarns int
	bus.Subscribe(false, func(ctx context.Context, e events.Event) {
		_ = ctx
		if e.Kind == events.ToolGuardrailWarn {
			guardWarns++
		}
	})
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{
			{Used: true, ToolName: "always_fail", Arguments: map[string]any{"x": float64(1)}},
			{Used: true, ToolName: "always_fail", Arguments: map[string]any{"x": float64(1)}},
			{Used: true, ToolName: "always_fail", Arguments: map[string]any{"x": float64(1)}},
		},
		finalReply: "done",
	}
	gcfg := &ToolGuardrailsConfig{
		Enabled: true, HardHalt: false,
		SameArgsFailureWarn: 2,
		SameToolFailureWarn: 100,
	}
	react := NewReActAgent(fake, mem, reg,
		WithReActEventBus(bus),
		WithReActToolGuardrails(gcfg),
		WithReActMaxSteps(10),
	)
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "x"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if guardWarns < 2 {
		t.Fatalf("expected at least 2 guardrail warn events, got %d", guardWarns)
	}
}

func TestReActAgent_ToolGuardrailHardHalt(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:        "always_fail",
		Description: "always fails",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(context.Context, map[string]any) (any, error) {
			return nil, errors.New("boom")
		},
	}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{
			{Used: true, ToolName: "always_fail", Arguments: map[string]any{"x": float64(1)}},
			{Used: true, ToolName: "always_fail", Arguments: map[string]any{"x": float64(1)}},
		},
		finalReply: "nope",
	}
	gcfg := &ToolGuardrailsConfig{
		Enabled: true, HardHalt: true,
		SameArgsFailureWarn: 2, SameArgsFailureHalt: 2,
		SameToolFailureWarn: 100, SameToolFailureHalt: 0,
	}
	react := NewReActAgent(fake, mem, reg, WithReActToolGuardrails(gcfg), WithReActMaxSteps(10))
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "x"}},
	})
	if err == nil || !errors.Is(err, ErrToolGuardrailHalt) {
		t.Fatalf("want ErrToolGuardrailHalt, got %v", err)
	}
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Trace == nil || !runErr.Trace.GuardrailHalt {
		t.Fatalf("expected RunError trace with GuardrailHalt, got %#v", err)
	}
	if len(runErr.Trace.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool records before halt, got %d", len(runErr.Trace.ToolCalls))
	}
	if runErr.Trace.GuardrailHaltMessage == nil {
		t.Fatal("expected GuardrailHaltMessage on trace")
	}
	if runErr.Trace.GuardrailHaltMessage.Metadata[model.MetadataKeySixathOrigin] != model.OriginGuardrailHalt {
		t.Fatalf("expected guardrail halt origin, got %#v", runErr.Trace.GuardrailHaltMessage.Metadata)
	}
}

// TestReActAgent_ToolCallUnwrapsToRealTool verifies tool_search bridge tool_call is unwrapped
// and the inner tool name appears in trace/events (integration: ReAct + tool discovery).
func TestReActAgent_ToolCallUnwrapsToRealTool(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:     true,
			ToolName: tool.ToolCallName,
			Arguments: map[string]any{
				"name": "calculator_add",
				"arguments": map[string]any{
					"a": float64(2),
					"b": float64(5),
				},
			},
		}},
		finalReply: "the result is 7",
	}
	reg := tool.NewRegistry()
	if err := tool.RegisterCalculatorTool(reg); err != nil {
		t.Fatalf("register calculator: %v", err)
	}
	cat := tool.BuildToolCatalog(context.Background(), reg)
	if err := tool.RegisterToolSearchTools(reg, tool.ToolSearchRegisterConfig{Registry: reg, Catalog: cat}); err != nil {
		t.Fatalf("register tool_search bridge: %v", err)
	}

	react := NewReActAgent(fake, mem, reg)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "2+5=?"}},
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if resp == nil || resp.Text != "the result is 7" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	trace, ok := resp.Metadata["trace"].(*RunTrace)
	if !ok || len(trace.ToolCalls) != 1 {
		t.Fatalf("expected one tool call in trace, got %#v", resp.Metadata)
	}
	call := trace.ToolCalls[0]
	if call.ToolName != "calculator_add" {
		t.Fatalf("trace should record inner tool name, got %#v", call)
	}
	if call.Result != float64(7) || call.Error != "" || !call.Allowed {
		t.Fatalf("unexpected inner tool execution: %#v", call)
	}
}

func TestReActAgent_ToolCallUnwrapsViaRunEvents(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeStreamingToolClient{
		fakeOpenAIClient: fakeOpenAIClient{finalReply: "the result is 7"},
		finalGen: &model.Generation{
			Raw: model.ToolStep{
				Used: true,
				ToolCalls: []model.ToolCall{{
					ID:   "call_bridge",
					Name: tool.ToolCallName,
					Arguments: map[string]any{
						"name": "calculator_add",
						"arguments": map[string]any{
							"a": float64(2),
							"b": float64(5),
						},
					},
				}},
			},
		},
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	cat := tool.BuildToolCatalog(context.Background(), reg)
	_ = tool.RegisterToolSearchTools(reg, tool.ToolSearchRegisterConfig{Registry: reg, Catalog: cat})

	react := NewReActAgent(fake, mem, reg)
	ch, err := react.RunEvents(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "2+5=?"}},
	})
	if err != nil {
		t.Fatalf("RunEvents error: %v", err)
	}

	var completed *ToolCallRecord
	for event := range ch {
		if event.Type == StreamEventToolCompleted {
			completed = event.ToolCall
		}
	}
	if completed == nil || completed.ToolName != "calculator_add" || completed.Result != float64(7) {
		t.Fatalf("expected completed inner tool call, got %#v", completed)
	}
}

func TestReActAgent_ToolCallUnwrapAppliesPermissionOnInnerTool(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	executed := false
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:     true,
			ToolName: tool.ToolCallName,
			Arguments: map[string]any{
				"name":      "calculator_add",
				"arguments": map[string]any{"a": float64(1), "b": float64(1)},
			},
		}},
		finalReply: "denied",
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	cat := tool.BuildToolCatalog(context.Background(), reg)
	_ = tool.RegisterToolSearchTools(reg, tool.ToolSearchRegisterConfig{Registry: reg, Catalog: cat})

	react := NewReActAgent(fake, mem, reg, WithReActPermissionPolicy(DenyTools("calculator_add")))
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "add"}},
	})
	if err == nil {
		t.Fatal("expected permission denied error")
	}
	if !errors.Is(err, ErrToolPermissionDenied) {
		t.Fatalf("expected ErrToolPermissionDenied, got %v", err)
	}
	if executed {
		t.Fatal("inner tool should not execute when denied")
	}
}

func TestReActAgent_ToolCallMissingInnerNameRecordsError(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:     true,
			ToolName: tool.ToolCallName,
			Arguments: map[string]any{
				"arguments": map[string]any{"a": float64(1)},
			},
		}},
		finalReply: "retry",
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	cat := tool.BuildToolCatalog(context.Background(), reg)
	_ = tool.RegisterToolSearchTools(reg, tool.ToolSearchRegisterConfig{Registry: reg, Catalog: cat})

	react := NewReActAgent(fake, mem, reg)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "calc"}},
	})
	if err != nil {
		t.Fatalf("Run should continue after recoverable tool error, got %v", err)
	}
	trace, ok := resp.Metadata["trace"].(*RunTrace)
	if !ok || len(trace.ToolCalls) != 1 {
		t.Fatalf("expected one tool record, got %#v", resp.Metadata)
	}
	if trace.ToolCalls[0].Error == "" || !strings.Contains(trace.ToolCalls[0].Error, "name is required") {
		t.Fatalf("expected missing name error, got %#v", trace.ToolCalls[0])
	}
}

func containsEvent(eventsSeen []events.Kind, want events.Kind) bool {
	for _, got := range eventsSeen {
		if got == want {
			return true
		}
	}
	return false
}

func countEvent(eventsSeen []events.Kind, want events.Kind) int {
	count := 0
	for _, got := range eventsSeen {
		if got == want {
			count++
		}
	}
	return count
}

func containsStreamEvent(eventsSeen []StreamEventType, want StreamEventType) bool {
	for _, got := range eventsSeen {
		if got == want {
			return true
		}
	}
	return false
}

// fakeSequencedStreamingToolClient 返回一串预排的 Generation：每次 ChatWithToolsStream
// 弹出队首（先流式 text，再交付该 gen）。用于验证 tools_stream 的多轮工具循环。
type fakeSequencedStreamingToolClient struct {
	fakeOpenAIClient
	gens  []*model.Generation
	texts []string
	calls int
}

func (f *fakeSequencedStreamingToolClient) ChatWithToolsStream(ctx context.Context, messages []model.Message, reg *tool.Registry, opts ...model.Option) (<-chan string, <-chan *model.Generation, error) {
	cfg := model.ApplyOptions(opts...)
	_ = model.PrepareChatContext(messages, cfg)
	_ = reg
	idx := f.calls
	f.calls++
	var text string
	if idx < len(f.texts) {
		text = f.texts[idx]
	}
	var gen *model.Generation
	if idx < len(f.gens) {
		gen = f.gens[idx]
	} else {
		gen = &model.Generation{Text: text, Raw: model.ToolStep{Used: false}}
	}
	ch := make(chan string)
	genCh := make(chan *model.Generation, 1)
	go func() {
		defer close(ch)
		defer close(genCh)
		for _, r := range text {
			select {
			case ch <- string(r):
			case <-ctx.Done():
				return
			}
		}
		genCh <- gen
	}()
	return ch, genCh, nil
}

// TestReActAgent_RunEvents_StreamingMultiRoundToolCalls 验证 tools_stream 路径支持多轮工具调用：
// 第 1 轮与第 2 轮各调用一次工具并真实执行，第 3 轮模型不再调用工具，其流式文本成为最终答案。
// 回归 runToolEvents 曾在首轮工具后强制 plain 收尾（single-round）导致第二轮工具被模型编造的 bug。
func TestReActAgent_RunEvents_StreamingMultiRoundToolCalls(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	toolGen := func(id string, a, b float64) *model.Generation {
		return &model.Generation{
			Raw: model.ToolStep{
				Used: true,
				ToolCalls: []model.ToolCall{{
					ID:        id,
					Name:      "calculator_add",
					Arguments: map[string]any{"a": a, "b": b},
				}},
			},
		}
	}
	fake := &fakeSequencedStreamingToolClient{
		gens: []*model.Generation{
			toolGen("c1", 1, 1), // round 1: tool
			toolGen("c2", 2, 2), // round 2: tool
			{Text: "final answer", Raw: model.ToolStep{Used: false}}, // round 3: stop
		},
		texts: []string{"", "", "final answer"},
	}

	react := NewReActAgent(fake, mem, reg, WithReActMaxSteps(10))
	ch, err := react.RunEvents(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "add twice"}},
	})
	if err != nil {
		t.Fatalf("RunEvents error: %v", err)
	}

	var got strings.Builder
	var completed []string
	var doneText string
	for event := range ch {
		if event.Type == StreamEventDelta {
			got.WriteString(event.Text)
		}
		if event.Type == StreamEventDone {
			doneText = event.Text
		}
		if event.Type == StreamEventToolCompleted {
			completed = append(completed, event.ToolCall.ToolName)
		}
	}
	if fake.calls != 3 {
		t.Fatalf("expected 3 streaming model rounds, got %d", fake.calls)
	}
	if len(completed) != 2 {
		t.Fatalf("expected 2 executed tool rounds, got %d (%v)", len(completed), completed)
	}
	if got.String() != "final answer" {
		t.Fatalf("expected final streamed text %q, got %q", "final answer", got.String())
	}
	if doneText != "final answer" {
		t.Fatalf("idle done Text %q, want gen.Text %q", doneText, "final answer")
	}
}

func TestReActAgent_ToolHookBeforeBlock_DoesNotExecute(t *testing.T) {
	executed := false
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:        "spy_tool",
		Description: "spy",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Execute: func(ctx context.Context, args map[string]any) (any, error) {
			executed = true
			return "should not run", nil
		},
	}); err != nil {
		t.Fatalf("register spy_tool: %v", err)
	}
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:      true,
			ToolName:  "spy_tool",
			Arguments: map[string]any{},
		}},
		finalReply: "blocked and continued",
	}
	hook := &recordingHook{name: "block", order: new([]string), before: func(context.Context, string, map[string]any) (map[string]any, error) {
		return nil, errors.New("blocked by test")
	}}
	bus := events.NewBus()
	var sawHookBlocked bool
	var sawPermissionDenied bool
	var sawRunError bool
	bus.Subscribe(false, func(ctx context.Context, ev events.Event) {
		switch ev.Kind {
		case events.HookBlocked:
			sawHookBlocked = true
		case events.PermissionDenied:
			sawPermissionDenied = true
		case events.RunError:
			sawRunError = true
		}
	})
	react := NewReActAgent(fake, nil, reg,
		WithReActToolHooks(hook),
		WithReActEventBus(bus),
		WithReActMaxSteps(3),
	)
	resp, err := react.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "go"}}})
	if err != nil {
		t.Fatal(err)
	}
	if executed {
		t.Fatal("Execute must not run when Before blocks")
	}
	if !sawHookBlocked {
		t.Fatal("expected events.HookBlocked on Bus")
	}
	if sawPermissionDenied {
		t.Fatal("hook block must not emit events.PermissionDenied on Bus")
	}
	if sawRunError {
		t.Fatal("hook block must not emit events.RunError on Bus")
	}
	tr, _ := resp.Metadata["trace"].(*RunTrace)
	if tr == nil {
		t.Fatal("expected RunTrace in Metadata")
	}
	found := false
	for _, rec := range tr.ToolCalls {
		if rec.ToolName != "spy_tool" {
			continue
		}
		found = true
		if !rec.Blocked {
			t.Fatal("expected Blocked=true")
		}
		content := toolMessageContent(rec)
		if !strings.Contains(content, `"blocked":true`) && !strings.Contains(content, `"blocked": true`) {
			t.Fatalf("tool message must include blocked:true, got %s", content)
		}
	}
	if !found {
		t.Fatal("expected spy_tool in Trace.ToolCalls")
	}
}

func TestReActAgent_RunEvents_HookBlockedNotPermissionDenied(t *testing.T) {
	executed := false
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:        "spy_tool",
		Description: "spy",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Execute: func(ctx context.Context, args map[string]any) (any, error) {
			executed = true
			return "should not run", nil
		},
	}); err != nil {
		t.Fatalf("register spy_tool: %v", err)
	}
	hook := &recordingHook{name: "block", order: new([]string), before: func(context.Context, string, map[string]any) (map[string]any, error) {
		return nil, errors.New("blocked by stream test")
	}}
	fake := &fakeSequencedStreamingToolClient{
		gens: []*model.Generation{
			{
				Raw: model.ToolStep{
					Used: true,
					ToolCalls: []model.ToolCall{{
						ID:        "call_blocked",
						Name:      "spy_tool",
						Arguments: map[string]any{},
					}},
				},
			},
			{Text: "after hook block", Raw: model.ToolStep{Used: false}},
		},
		texts: []string{"", "after hook block"},
	}
	react := NewReActAgent(fake, nil, reg,
		WithReActToolHooks(hook),
		WithReActMaxSteps(3),
	)
	ch, err := react.RunEvents(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatalf("RunEvents error: %v", err)
	}

	var types []StreamEventType
	var blocked *ToolCallRecord
	for event := range ch {
		types = append(types, event.Type)
		if event.Type == StreamEventHookBlocked {
			blocked = event.ToolCall
		}
	}
	if executed {
		t.Fatal("Execute must not run when Before blocks")
	}
	if !containsStreamEvent(types, StreamEventHookBlocked) {
		t.Fatalf("expected StreamEventHookBlocked, got %#v", types)
	}
	if containsStreamEvent(types, StreamEventPermissionDenied) {
		t.Fatalf("hook block must not be emitted as PermissionDenied, got %#v", types)
	}
	if containsStreamEvent(types, StreamEventError) {
		t.Fatalf("hook block must not abort run with StreamEventError, got %#v", types)
	}
	if !containsStreamEvent(types, StreamEventDone) {
		t.Fatalf("expected StreamEventDone after hook block, got %#v", types)
	}
	if blocked == nil || blocked.ToolName != "spy_tool" || !blocked.Blocked || blocked.Allowed {
		t.Fatalf("unexpected hook_blocked tool event: %#v", blocked)
	}
}

func TestReActAgent_EmptyToolHooks_SameAsBaseline(t *testing.T) {
	run := func(t *testing.T, opts ...ReActOption) *RunTrace {
		t.Helper()
		mem := memory.NewBufferMemory(5)
		fake := &fakeOpenAIClient{
			toolSteps: []model.ToolStep{{
				Used:      true,
				ToolName:  "calculator_add",
				Arguments: map[string]any{"a": float64(1), "b": float64(3)},
			}},
			finalReply: "the result is 4",
		}
		reg := tool.NewRegistry()
		if err := tool.RegisterCalculatorTool(reg); err != nil {
			t.Fatalf("register calculator tool error: %v", err)
		}
		react := NewReActAgent(fake, mem, reg, opts...)
		resp, err := react.Run(context.Background(), &Request{
			Messages: []model.Message{{Role: "user", Content: "1+3=?"}},
		})
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		if resp == nil || resp.Text != "the result is 4" {
			t.Fatalf("unexpected response: %#v", resp)
		}
		tr, ok := resp.Metadata["trace"].(*RunTrace)
		if !ok || tr == nil {
			t.Fatalf("expected RunTrace, got %#v", resp.Metadata)
		}
		return tr
	}

	for name, opts := range map[string][]ReActOption{
		"baseline":   nil,
		"emptyHooks": {WithReActToolHooks()},
	} {
		t.Run(name, func(t *testing.T) {
			tr := run(t, opts...)
			if len(tr.ToolCalls) != 1 {
				t.Fatalf("expected one tool call, got %#v", tr.ToolCalls)
			}
			rec := tr.ToolCalls[0]
			if rec.Error != "" || rec.Blocked || rec.Result != float64(4) {
				t.Fatalf("unexpected record: %#v", rec)
			}
		})
	}
}

func TestReActAgent_MessagesSnapshotAfterToolRun(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:      true,
			ToolName:  "calculator_add",
			Arguments: map[string]any{"a": float64(1), "b": float64(3)},
		}},
		finalReply: "the result is 4",
	}
	reg := tool.NewRegistry()
	if err := tool.RegisterCalculatorTool(reg); err != nil {
		t.Fatalf("register calculator tool error: %v", err)
	}

	react := NewReActAgent(fake, mem, reg)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "1+3=?"}},
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	assertMessagesSnapshotHasToolTrajectory(t, resp.Messages)
}

func TestReActAgent_MessagesSnapshotOnStreamDone(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:      true,
			ToolName:  "calculator_add",
			Arguments: map[string]any{"a": float64(1), "b": float64(3)},
		}},
		finalReply: "the result is 4",
	}
	reg := tool.NewRegistry()
	if err := tool.RegisterCalculatorTool(reg); err != nil {
		t.Fatalf("register calculator tool error: %v", err)
	}

	react := NewReActAgent(fake, mem, reg)
	ch, err := react.RunEvents(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "1+3=?"}},
	})
	if err != nil {
		t.Fatalf("RunEvents error: %v", err)
	}
	var doneMsgs []model.Message
	var doneText string
	for event := range ch {
		if event.Type == StreamEventDone {
			doneMsgs = event.Messages
			doneText = event.Text
		}
	}
	if doneText != "the result is 4" {
		t.Fatalf("idle done Text %q, want gen.Text %q", doneText, "the result is 4")
	}
	assertMessagesSnapshotHasToolTrajectory(t, doneMsgs)
}

func assertMessagesSnapshotHasToolTrajectory(t *testing.T, msgs []model.Message) {
	t.Helper()
	if len(msgs) == 0 {
		t.Fatalf("expected non-empty Messages snapshot")
	}
	for i := 0; i < len(msgs)-1; i++ {
		asst := msgs[i]
		toolMsg := msgs[i+1]
		if !strings.EqualFold(asst.Role, "assistant") || !strings.EqualFold(toolMsg.Role, "tool") {
			continue
		}
		if asst.Metadata == nil {
			continue
		}
		if _, ok := asst.Metadata["tool_calls"]; !ok {
			continue
		}
		if toolMsg.Metadata == nil || toolMsg.Metadata["tool_name"] != "calculator_add" {
			t.Fatalf("expected tool result for calculator_add after tool_calls, got %#v", toolMsg)
		}
		return
	}
	t.Fatalf("expected assistant tool_calls followed by tool result in order, got %#v", msgs)
}
