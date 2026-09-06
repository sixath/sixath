package harness

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func TestReActAgent_MemoryOrchestratorInjectsPrefetchBeforeIncoming(t *testing.T) {
	orch := memory.NewOrchestrator()
	_ = orch.RegisterBackend(&agentPrefetchBackend{})

	mem := memory.NewBufferMemory(10)
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	fake := &fakeOpenAIClient{
		toolSteps:  []model.ToolStep{{Used: false}},
		finalReply: "ok",
	}

	react := NewReActAgent(fake, mem, reg, WithReActMemoryOrchestrator(orch))
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "1+1"}},
		Metadata: map[string]any{"session_id": "sess-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.lastToolMessages) < 2 {
		t.Fatalf("expected prefetch + user in messages to model, got len=%d", len(fake.lastToolMessages))
	}
	foundFence := false
	for _, m := range fake.lastToolMessages {
		if strings.Contains(m.Content, "sixath-memory-context") {
			foundFence = true
			break
		}
	}
	if !foundFence {
		t.Fatalf("expected fence in model messages, got %#v", fake.lastToolMessages)
	}
}

type capturePrefetchBackend struct {
	got memory.PrefetchQuery
}

func (c *capturePrefetchBackend) Name() string { return "capture" }

func (c *capturePrefetchBackend) Prefetch(ctx context.Context, q memory.PrefetchQuery) ([]memory.PrefetchPart, error) {
	_ = ctx
	c.got = q
	return nil, nil
}

func TestReActAgent_PrefetchQueryCarriesRecentIdentityAndContextKeys(t *testing.T) {
	orch := memory.NewOrchestrator()
	cap := &capturePrefetchBackend{}
	_ = orch.RegisterBackend(cap)
	mem := memory.NewBufferMemory(10)
	_ = mem.Add(context.Background(), memory.Entry{Message: model.Message{Role: "assistant", Content: "history-a"}})
	reg := tool.NewRegistry()
	fake := &fakeOpenAIClient{toolSteps: []model.ToolStep{{Used: false}}, finalReply: "ok"}
	react := NewReActAgent(fake, mem, reg, WithReActMemoryOrchestrator(orch))
	ctx := context.WithValue(context.Background(), tool.ContextKeyWorkspaceRoot, "D:/ws")
	ctx = context.WithValue(ctx, tool.ContextKeyAgentID, "agent-from-ctx")
	_, err := react.Run(ctx, &Request{
		Messages: []model.Message{{Role: "user", Content: "new-q"}},
		Metadata: map[string]any{"session_id": "sess-ctx"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.got.UserMessage != "new-q" {
		t.Fatalf("user message mismatch: %#v", cap.got)
	}
	if cap.got.Identity != "sess-ctx" {
		t.Fatalf("identity should fallback to session_id, got %#v", cap.got)
	}
	if cap.got.WorkspaceRoot != "D:/ws" || cap.got.AgentID != "agent-from-ctx" {
		t.Fatalf("context keys not propagated: %#v", cap.got)
	}
	if len(cap.got.Recent) < 2 {
		t.Fatalf("expected history+incoming in Recent, got %d %#v", len(cap.got.Recent), cap.got.Recent)
	}
}

func TestPrefetchQueryFrom_ReadsUserIDFromContextAndMetadata(t *testing.T) {
	ctx := context.WithValue(context.Background(), tool.ContextKeyUserID, "user-ctx")
	q := prefetchQueryFrom(ctx, &Request{
		Messages: []model.Message{{Role: "user", Content: "hi"}},
		Metadata: map[string]any{"user_id": "user-meta"},
	}, nil)
	if q.UserID != "user-meta" {
		t.Fatalf("UserID = %q, want metadata override user-meta", q.UserID)
	}
	q2 := prefetchQueryFrom(ctx, &Request{
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	}, nil)
	if q2.UserID != "user-ctx" {
		t.Fatalf("UserID = %q, want context user-ctx", q2.UserID)
	}
}

type agentPrefetchBackend struct{}

func (agentPrefetchBackend) Name() string { return "fake" }

func (agentPrefetchBackend) Prefetch(ctx context.Context, q memory.PrefetchQuery) ([]memory.PrefetchPart, error) {
	_ = ctx
	if q.SessionID != "sess-1" {
		return nil, nil
	}
	return []memory.PrefetchPart{{Content: "recalled fact"}}, nil
}

func TestReActAgent_PrefetchFailOpenRecordsTraceSkip(t *testing.T) {
	orch := memory.NewOrchestrator()
	_ = orch.RegisterBackend(errPrefetchBackend{})
	mem := memory.NewBufferMemory(10)
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	fake := &fakeOpenAIClient{
		toolSteps:  []model.ToolStep{{Used: false}},
		finalReply: "ok",
	}
	react := NewReActAgent(fake, mem, reg, WithReActMemoryOrchestrator(orch))
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "1+1"}},
		Metadata: map[string]any{"session_id": "sess-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := resp.Metadata["trace"].(*RunTrace)
	if !ok || tr == nil || !tr.PrefetchSkipped || tr.PrefetchSkipReason != string(memory.PrefetchSkipBackendError) {
		t.Fatalf("expected prefetch skip trace, got %#v ok=%v", tr, ok)
	}
}

func TestReActAgent_PrefetchFailClosedPropagatesError(t *testing.T) {
	orch := memory.NewOrchestrator()
	orch.PrefetchFailClosed = true
	_ = orch.RegisterBackend(errPrefetchBackend{})
	mem := memory.NewBufferMemory(10)
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	fake := &fakeOpenAIClient{
		toolSteps:  []model.ToolStep{{Used: false}},
		finalReply: "ok",
	}
	react := NewReActAgent(fake, mem, reg, WithReActMemoryOrchestrator(orch))
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "1+1"}},
	})
	if err == nil {
		t.Fatal("expected error from fail-closed prefetch")
	}
}

type errPrefetchBackend struct{}

func (errPrefetchBackend) Name() string { return "err" }

func (errPrefetchBackend) Prefetch(ctx context.Context, q memory.PrefetchQuery) ([]memory.PrefetchPart, error) {
	_ = ctx
	_ = q
	return nil, errors.New("backend down")
}
