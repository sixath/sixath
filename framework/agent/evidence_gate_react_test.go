package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func TestReActEvidenceGate_SoftInjectThenIncomplete(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{finalReply: "root cause is OOM without tooling"}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	bus := events.NewBus()
	var incompleteN int
	var mu sync.Mutex
	bus.Subscribe(false, func(ctx context.Context, e events.Event) {
		if e.Kind != events.EvidenceIncomplete {
			return
		}
		mu.Lock()
		incompleteN++
		mu.Unlock()
	})

	react := NewReActAgent(fake, mem, reg,
		WithReActMaxSteps(3),
		WithReActEventBus(bus),
		WithReActEvidenceGate(EvidenceGateConfig{Enabled: true}),
	)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "why is svc down?"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fake.toolCalls < 2 {
		t.Fatalf("expected Soft inject retry (toolCalls>=2), got %d", fake.toolCalls)
	}
	tr, _ := resp.Metadata["trace"].(*RunTrace)
	if tr == nil || tr.EvidenceNudges != 1 {
		t.Fatalf("EvidenceNudges want 1, got %#v", tr)
	}
	if resp.Metadata["evidence_incomplete"] != true {
		t.Fatalf("expected evidence_incomplete metadata, got %#v", resp.Metadata)
	}
	mu.Lock()
	n := incompleteN
	mu.Unlock()
	if n != 1 {
		t.Fatalf("EvidenceIncomplete events=%d want 1", n)
	}
	foundPrompt := false
	for _, m := range fake.lastToolMessages {
		if m.Role == "user" && strings.Contains(m.Content, "jaeger_trace") {
			foundPrompt = true
			break
		}
	}
	if !foundPrompt {
		t.Fatalf("expected Soft inject prompt in second-round messages")
	}
}

func TestReActEvidenceGate_InsufficientEvidenceTextAllows(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{finalReply: "本次无法定位，证据不足。"}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	react := NewReActAgent(fake, mem, reg,
		WithReActMaxSteps(3),
		WithReActEvidenceGate(EvidenceGateConfig{Enabled: true}),
	)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "why?"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fake.toolCalls != 1 {
		t.Fatalf("expected single model call (no inject), got %d", fake.toolCalls)
	}
	tr, _ := resp.Metadata["trace"].(*RunTrace)
	if tr != nil && tr.EvidenceNudges != 0 {
		t.Fatalf("EvidenceNudges want 0, got %d", tr.EvidenceNudges)
	}
	if resp.Metadata["evidence_incomplete"] == true {
		t.Fatal("证据不足 text must not mark evidence_incomplete")
	}
}

func TestReActEvidenceGate_ForceFinalSoftOnlyMetadata(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:      true,
			ToolName:  "calculator_add",
			Arguments: map[string]any{"a": float64(1), "b": float64(1)},
		}},
		finalReply: "forced conclusion without jaeger",
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	bus := events.NewBus()
	var incompleteN int
	bus.Subscribe(false, func(ctx context.Context, e events.Event) {
		if e.Kind == events.EvidenceIncomplete {
			incompleteN++
		}
	})

	react := NewReActAgent(fake, mem, reg,
		WithReActMaxSteps(1),
		WithReActEventBus(bus),
		WithReActEvidenceGate(EvidenceGateConfig{Enabled: true}),
	)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "summarize"}},
	})
	if err != nil {
		t.Fatalf("expected Soft forceFinal success, got %v", err)
	}
	if resp == nil || resp.Text != "forced conclusion without jaeger" {
		t.Fatalf("unexpected resp %#v", resp)
	}
	tr, _ := resp.Metadata["trace"].(*RunTrace)
	if tr == nil {
		t.Fatal("missing trace")
	}
	if tr.EvidenceNudges != 0 {
		t.Fatalf("forceFinal must not Soft-inject; EvidenceNudges=%d", tr.EvidenceNudges)
	}
	if resp.Metadata["evidence_incomplete"] != true {
		t.Fatalf("expected evidence_incomplete on forceFinal Soft, got %#v", resp.Metadata)
	}
	if incompleteN != 1 {
		t.Fatalf("EvidenceIncomplete events=%d want 1", incompleteN)
	}
	// ChatWithTools once (tool step) + Chat once (force summary); no extra tool loop for Soft.
	if fake.toolCalls != 1 {
		t.Fatalf("toolCalls=%d want 1 (no Soft continue)", fake.toolCalls)
	}
	if fake.chatCalls != 1 {
		t.Fatalf("chatCalls=%d want 1 (forceFinal only)", fake.chatCalls)
	}
}

func TestReActEvidenceGate_HardHaltForceFinal(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:      true,
			ToolName:  "calculator_add",
			Arguments: map[string]any{"a": float64(1), "b": float64(1)},
		}},
		finalReply: "hard halt conclusion",
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	react := NewReActAgent(fake, mem, reg,
		WithReActMaxSteps(1),
		WithReActEvidenceGate(EvidenceGateConfig{Enabled: true, HardHalt: true}),
	)
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "summarize"}},
	})
	if !errors.Is(err, ErrEvidenceGateHalt) {
		t.Fatalf("expected ErrEvidenceGateHalt, got %v", err)
	}
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Trace == nil {
		t.Fatalf("expected RunError with trace, got %T %v", err, err)
	}
}

func TestReActEvidenceGate_DisabledByDefault(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{finalReply: "premature answer"}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)

	react := NewReActAgent(fake, mem, reg, WithReActMaxSteps(2))
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "why?"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fake.toolCalls != 1 {
		t.Fatalf("disabled gate must not inject, toolCalls=%d", fake.toolCalls)
	}
	if resp.Metadata["evidence_incomplete"] == true {
		t.Fatal("disabled gate must not mark incomplete")
	}
}
