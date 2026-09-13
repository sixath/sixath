package agent

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

func TestRun_AggregatesTokenUsageIntoRunTrace(t *testing.T) {
	m := &fakeTokenModel{text: "hello", usage: &model.TokenUsage{InputTokens: 11, OutputTokens: 7}}
	a := NewReActAgent(m, nil, nil, WithReActMaxSteps(2))

	resp, err := a.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tr, ok := resp.Metadata["trace"].(*RunTrace)
	if !ok || tr == nil {
		t.Fatalf("trace missing: %#v", resp.Metadata)
	}
	if tr.ModelCalls != 1 {
		t.Fatalf("ModelCalls=%d want 1", tr.ModelCalls)
	}
	if tr.InputTokens != 11 || tr.OutputTokens != 7 {
		t.Fatalf("usage not aggregated: input=%d output=%d", tr.InputTokens, tr.OutputTokens)
	}
}

func TestBuildTurnTrace_CarriesTokenUsage(t *testing.T) {
	tt := BuildTurnTrace(TurnTraceMeta{SessionID: "s1", AgentID: "a1", RequestID: "r1"}, &RunTrace{
		RequestID:    "r1",
		ModelCalls:   3,
		InputTokens:  120,
		OutputTokens: 45,
	})
	if tt == nil {
		t.Fatal("nil turn trace")
	}
	if tt.ModelCalls != 3 || tt.InputTokens != 120 || tt.OutputTokens != 45 {
		t.Fatalf("usage not carried: %+v", tt)
	}
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
