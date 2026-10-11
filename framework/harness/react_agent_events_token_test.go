package harness

import (
	"context"
	"testing"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/model"
)

// fakeTokenModel 是仅实现 model.Model 的简单假模型，Chat 返回已知 TokenUsage，
// 用于验证 ModelResponded 事件是否携带 token 用量。它不实现 ToolCallingModel /
// StreamingModel，因此配合 tools=nil 会走 runPlainEvents 的非流式分支。
type fakeTokenModel struct {
	text  string
	usage *model.TokenUsage
}

func (f *fakeTokenModel) Generate(_ context.Context, _ string, _ ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: f.text, TokenUsage: f.usage}, nil
}

func (f *fakeTokenModel) Chat(_ context.Context, _ []model.Message, _ ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: f.text, TokenUsage: f.usage}, nil
}

func (f *fakeTokenModel) Embed(_ context.Context, texts []string, _ ...model.Option) ([]model.Embedding, error) {
	return make([]model.Embedding, len(texts)), nil
}

func TestRunEvents_ModelRespondedIncludesTokenUsage(t *testing.T) {
	bus := events.NewBus()
	var gotInput, gotOutput int
	var sawResponded bool
	bus.Subscribe(false, func(_ context.Context, e events.Event) {
		if e.Kind == events.ModelResponded {
			sawResponded = true
			if v, ok := e.Payload["input_tokens"].(int); ok {
				gotInput = v
			}
			if v, ok := e.Payload["output_tokens"].(int); ok {
				gotOutput = v
			}
		}
	})

	m := &fakeTokenModel{text: "hello", usage: &model.TokenUsage{InputTokens: 11, OutputTokens: 7}}
	a := NewReActAgent(m, nil, nil, WithReActEventBus(bus), WithReActMaxSteps(2))

	ch, err := a.RunEvents(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("RunEvents: %v", err)
	}
	for range ch {
	}

	if !sawResponded {
		t.Fatal("expected ModelResponded event")
	}
	if gotInput != 11 || gotOutput != 7 {
		t.Fatalf("token usage not propagated: input=%d output=%d", gotInput, gotOutput)
	}
}

func TestModelRespondedPayload_IncludesReasoningAndAnswer(t *testing.T) {
	tr := &RunTrace{RequestID: "r1"}
	gen := model.Generation{
		Raw:          model.ToolStep{Used: true, ReasoningContent: "because x"},
		Text:         "do the thing",
		FinishReason: "tool_calls",
		TokenUsage:   &model.TokenUsage{InputTokens: 1, OutputTokens: 1},
	}
	p := tr.modelRespondedPayload(gen, 0, "tools", []model.Message{{Role: "user", Content: "go"}})
	if p["reasoning"] != "because x" {
		t.Fatalf("expected reasoning, got %#v", p)
	}
	if p["answer"] != "do the thing" {
		t.Fatalf("expected answer, got %#v", p)
	}
	if p["finish_reason"] != "tool_calls" {
		t.Fatalf("expected finish_reason, got %#v", p)
	}
	if len(tr.ModelCalls) == 0 {
		t.Fatal("expected model call recorded")
	}
}
