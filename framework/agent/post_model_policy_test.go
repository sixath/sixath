package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

type finishAllPolicy struct{}

func (finishAllPolicy) Evaluate(context.Context, PostModelPolicyInput) PostModelPolicyResult {
	return PostModelPolicyResult{Decision: PostModelFinish, Reason: "test_finish"}
}

type filterDropAllPolicy struct{}

func (filterDropAllPolicy) Evaluate(context.Context, PostModelPolicyInput) PostModelPolicyResult {
	return PostModelPolicyResult{Decision: PostModelFilter, ToolCalls: nil, Reason: "test_filter_empty"}
}

func TestReActAgent_PostModelPolicyFinishDiscardsTools(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used: true,
			ToolCalls: []model.ToolCall{{
				ID:        "call_1",
				Name:      "calculator_add",
				Arguments: map[string]any{"a": float64(1), "b": float64(3)},
			}},
		}},
		finalReply: "模块分析已完成。如有需要请告诉我。",
	}
	reg := tool.NewRegistry()
	if err := tool.RegisterCalculatorTool(reg); err != nil {
		t.Fatal(err)
	}
	react := NewReActAgent(fake, mem, reg, WithReActPostModelPolicy(finishAllPolicy{}))
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "分析 cloudgame 模块"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || resp.Text != "模块分析已完成。如有需要请告诉我。" {
		t.Fatalf("unexpected resp: %#v", resp)
	}
	trace, _ := resp.Metadata["trace"].(*RunTrace)
	if trace == nil {
		t.Fatal("missing trace")
	}
	if len(trace.ToolCalls) != 0 {
		t.Fatalf("expected no tool execution, got %#v", trace.ToolCalls)
	}
	found := false
	for _, e := range trace.Errors {
		if strings.Contains(e, "post_model_policy:finish:test_finish") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected policy finish marker in trace.Errors, got %#v", trace.Errors)
	}
}

func TestReActAgent_PostModelPolicyFilterEmptyFinishes(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used: true,
			ToolCalls: []model.ToolCall{{
				ID:   "call_1",
				Name: "calculator_add",
				Arguments: map[string]any{
					"a": float64(1), "b": float64(2),
				},
			}},
		}},
		finalReply: "done",
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	react := NewReActAgent(fake, mem, reg, WithReActPostModelPolicy(filterDropAllPolicy{}))
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "1+2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "done" {
		t.Fatalf("got %q", resp.Text)
	}
	trace, _ := resp.Metadata["trace"].(*RunTrace)
	if len(trace.ToolCalls) != 0 {
		t.Fatalf("expected no tools, got %#v", trace.ToolCalls)
	}
}

func TestReActAgent_WithoutPostModelPolicyStillRunsTools(t *testing.T) {
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
	_ = tool.RegisterCalculatorTool(reg)
	react := NewReActAgent(fake, mem, reg)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "1+3=?"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	trace, _ := resp.Metadata["trace"].(*RunTrace)
	if len(trace.ToolCalls) != 1 {
		t.Fatalf("expected tool run, got %#v", trace.ToolCalls)
	}
}
