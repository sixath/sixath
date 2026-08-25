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

type retryAllPolicy struct{}

func (retryAllPolicy) Evaluate(context.Context, PostModelPolicyInput) PostModelPolicyResult {
	return PostModelPolicyResult{Decision: PostModelRetry, Reason: "family_dropped_all", Prompt: "use rca only"}
}

func TestReActAgent_PostModelPolicyRetryInjectsAndContinues(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used: true,
			ToolCalls: []model.ToolCall{{
				ID:        "call_1",
				Name:      "calculator_add",
				Arguments: map[string]any{"a": float64(1), "b": float64(2)},
			}},
		}},
		finalReply: "ok",
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	react := NewReActAgent(fake, mem, reg, WithReActPostModelPolicy(retryAllPolicy{}), WithReActMaxSteps(5))
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "1+2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "ok" {
		t.Fatalf("got %q", resp.Text)
	}
	if fake.toolCalls < 2 {
		t.Fatalf("expected a retry model round, toolCalls=%d", fake.toolCalls)
	}
	found := false
	for _, m := range fake.lastToolMessages {
		if m.Role == "user" && strings.Contains(m.Content, "use rca only") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected retry prompt in messages, last=%#v", fake.lastToolMessages)
	}
	trace, _ := resp.Metadata["trace"].(*RunTrace)
	if len(trace.ToolCalls) != 0 {
		t.Fatalf("retry must not execute tools, got %#v", trace.ToolCalls)
	}
	saw := false
	for _, e := range trace.Errors {
		if strings.Contains(e, "post_model_policy:retry:family_dropped_all") {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("trace.Errors=%v", trace.Errors)
	}
}

type idleRetryOnce struct{}

func (idleRetryOnce) Evaluate(context.Context, PostModelPolicyInput) PostModelPolicyResult {
	return PostModelPolicyResult{Decision: PostModelContinue}
}

func (idleRetryOnce) EvaluateIdle(_ context.Context, in PostModelPolicyInput) PostModelPolicyResult {
	if in.Trace != nil && in.Trace.GoalDriftNudges > 0 {
		return PostModelPolicyResult{Decision: PostModelContinue}
	}
	if in.Trace != nil {
		in.Trace.GoalDriftNudges++
	}
	return PostModelPolicyResult{Decision: PostModelRetry, Reason: "goal_drift", Prompt: "仍回答原问题"}
}

func TestReActAgent_IdlePolicyRetryThenFinish(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps:    []model.ToolStep{{Used: false}, {Used: false}},
		plainReplies: []string{"请提供 flow_id", "access-service 无命中"},
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	react := NewReActAgent(fake, mem, reg, WithReActPostModelPolicy(idleRetryOnce{}), WithReActMaxSteps(5))
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "查 access-service"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || resp.Text != "access-service 无命中" {
		t.Fatalf("unexpected resp: %#v", resp)
	}
	trace, _ := resp.Metadata["trace"].(*RunTrace)
	if trace == nil {
		t.Fatal("missing trace")
	}
	if trace.GoalDriftNudges != 1 {
		t.Fatalf("GoalDriftNudges=%d", trace.GoalDriftNudges)
	}
	saw := false
	for _, e := range trace.Errors {
		if strings.Contains(e, "post_model_policy:retry:goal_drift") {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("trace.Errors=%v", trace.Errors)
	}
}

func TestReActAgent_FinishPolicyWithoutIdleUnchanged(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{finalReply: "请提供 flow_id"}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	react := NewReActAgent(fake, mem, reg, WithReActPostModelPolicy(finishAllPolicy{}))
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "查 access-service"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || resp.Text != "请提供 flow_id" {
		t.Fatalf("unexpected resp: %#v", resp)
	}
	if fake.toolCalls != 1 {
		t.Fatalf("idle-less policy must finish immediately, toolCalls=%d", fake.toolCalls)
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
