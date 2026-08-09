package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

const parallelSlowToolDelay = 120 * time.Millisecond

func registerSlowReadTool(t *testing.T, reg *tool.Registry, name string, counter *atomic.Int32) {
	t.Helper()
	if err := reg.Register(tool.Tool{
		Name:        name,
		Description: "slow read for parallel tests",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Execute: func(ctx context.Context, args map[string]any) (any, error) {
			select {
			case <-time.After(parallelSlowToolDelay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			if counter != nil {
				counter.Add(1)
			}
			return map[string]any{"tool": name, "ok": true}, nil
		},
	}); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
}

func TestReActParallelTools_DefaultOff(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	reg := tool.NewRegistry()
	registerSlowReadTool(t, reg, "slow_read_a", nil)
	registerSlowReadTool(t, reg, "slow_read_b", nil)

	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used: true,
			ToolCalls: []model.ToolCall{
				{ID: "call_a", Name: "slow_read_a", Arguments: map[string]any{}},
				{ID: "call_b", Name: "slow_read_b", Arguments: map[string]any{}},
			},
		}},
		finalReply: "done",
	}

	react := NewReActAgent(fake, mem, reg, WithReActMaxSteps(3))
	start := time.Now()
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "read both"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	elapsed := time.Since(start)
	tr, _ := resp.Metadata["trace"].(*RunTrace)
	if tr == nil {
		t.Fatal("missing trace")
	}
	if tr.ParallelTools {
		t.Fatal("default ParallelTools must be false")
	}
	// Serial: two slow tools should take at least ~2x delay.
	if elapsed < 2*parallelSlowToolDelay-20*time.Millisecond {
		t.Fatalf("expected sequential timing >= %v, got %v", 2*parallelSlowToolDelay, elapsed)
	}
	if len(tr.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(tr.ToolCalls))
	}
	if tr.ToolCalls[0].ToolCallID != "call_a" || tr.ToolCalls[1].ToolCallID != "call_b" {
		t.Fatalf("tool call order mismatch: %#v", tr.ToolCalls)
	}
}

func TestReActParallelTools_ParallelOnTwoSlowReads(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	reg := tool.NewRegistry()
	registerSlowReadTool(t, reg, "slow_read_a", nil)
	registerSlowReadTool(t, reg, "slow_read_b", nil)

	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used: true,
			ToolCalls: []model.ToolCall{
				{ID: "call_a", Name: "slow_read_a", Arguments: map[string]any{}},
				{ID: "call_b", Name: "slow_read_b", Arguments: map[string]any{}},
			},
		}},
		finalReply: "done",
	}

	react := NewReActAgent(fake, mem, reg,
		WithReActMaxSteps(3),
		WithReActParallelTools(true),
		WithReActMaxParallel(4),
	)
	start := time.Now()
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "read both"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	elapsed := time.Since(start)
	tr, _ := resp.Metadata["trace"].(*RunTrace)
	if tr == nil {
		t.Fatal("missing trace")
	}
	if !tr.ParallelTools {
		t.Fatal("expected ParallelTools=true when parallel batch ran")
	}
	// Parallel: wall time should be less than sum of both delays.
	if elapsed >= 2*parallelSlowToolDelay-10*time.Millisecond {
		t.Fatalf("expected parallel wall time < %v, got %v", 2*parallelSlowToolDelay, elapsed)
	}
	if len(tr.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(tr.ToolCalls))
	}
	if tr.ToolCalls[0].ToolCallID != "call_a" || tr.ToolCalls[1].ToolCallID != "call_b" {
		t.Fatalf("slot order must match tool_calls order: %#v", tr.ToolCalls)
	}
}

func TestReActParallelTools_SequentialToolForcesSerial(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	reg := tool.NewRegistry()
	registerSlowReadTool(t, reg, "slow_read_a", nil)
	registerSlowReadTool(t, reg, "slow_read_b", nil)
	if err := reg.Register(tool.Tool{
		Name:               "terminal",
		Description:        "sequential stub",
		RequiresSequential: true,
		Parameters:         map[string]any{"type": "object", "properties": map[string]any{}},
		Execute: func(ctx context.Context, args map[string]any) (any, error) {
			return "ok", nil
		},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}

	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used: true,
			ToolCalls: []model.ToolCall{
				{ID: "call_a", Name: "slow_read_a", Arguments: map[string]any{}},
				{ID: "call_term", Name: "terminal", Arguments: map[string]any{}},
			},
		}},
		finalReply: "done",
	}

	react := NewReActAgent(fake, mem, reg,
		WithReActMaxSteps(3),
		WithReActParallelTools(true),
	)
	start := time.Now()
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "mixed batch"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	elapsed := time.Since(start)
	tr, _ := resp.Metadata["trace"].(*RunTrace)
	if tr == nil {
		t.Fatal("missing trace")
	}
	if tr.ParallelTools {
		t.Fatal("RequiresSequential in batch must force serial; ParallelTools=false")
	}
	// Serial: one slow tool + instant terminal still waits full slow delay.
	if elapsed < parallelSlowToolDelay-20*time.Millisecond {
		t.Fatalf("expected serial timing >= %v, got %v", parallelSlowToolDelay, elapsed)
	}
	if len(tr.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(tr.ToolCalls))
	}
	if tr.ToolCalls[0].ToolCallID != "call_a" || tr.ToolCalls[1].ToolCallID != "call_term" {
		t.Fatalf("order mismatch: %#v", tr.ToolCalls)
	}
}
