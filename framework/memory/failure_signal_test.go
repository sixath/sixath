package memory

import (
	"context"
	"testing"
	"time"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/tool"
)

func TestFailureSignalFromEvent_ToolFailed(t *testing.T) {
	ctx := context.WithValue(context.Background(), tool.ContextKeyAgentID, "zone-4100-agent")
	ctx = context.WithValue(ctx, tool.ContextKeySessionID, "sess-1")
	e := events.Event{
		Kind: events.ToolFailed,
		Payload: map[string]any{
			"tool":  "ssh_exec",
			"error": "exit 1",
			"step":  3,
		},
		At: time.Unix(100, 0).UTC(),
	}
	sig, ok := FailureSignalFromEvent(ctx, e)
	if !ok {
		t.Fatal("expected signal")
	}
	if sig.Code != FailureCodeToolFailed {
		t.Fatalf("code: %s", sig.Code)
	}
	if sig.AgentID != "zone-4100-agent" || sig.SessionID != "sess-1" {
		t.Fatalf("identity: %#v", sig)
	}
	if sig.TaskFamily != "zone-4100-agent" {
		t.Fatalf("task family default: %q", sig.TaskFamily)
	}
	if sig.ToolName != "ssh_exec" || sig.Evidence["error"] == "" {
		t.Fatalf("tool/evidence: %#v", sig)
	}
}

func TestFailureSignalFromEvent_ToolGuardrailWarn(t *testing.T) {
	e := events.Event{
		Kind: events.ToolGuardrailWarn,
		Payload: map[string]any{
			"rule":   "same_tool_failure",
			"tool":   "ssh_exec",
			"streak": 3,
		},
	}
	sig, ok := FailureSignalFromEvent(context.Background(), e)
	if !ok || sig.Code != FailureCodeToolRepeatFail {
		t.Fatalf("got ok=%v sig=%#v", ok, sig)
	}
	if sig.Evidence["rule"] != "same_tool_failure" {
		t.Fatalf("evidence: %#v", sig.Evidence)
	}
}

func TestFailureSignalFromEvent_IgnoresUnrelated(t *testing.T) {
	_, ok := FailureSignalFromEvent(context.Background(), events.Event{Kind: events.ToolExecuted})
	if ok {
		t.Fatal("must ignore")
	}
}

func TestFailureSignalFromEvent_PermissionDenied(t *testing.T) {
	e := events.Event{
		Kind:    events.PermissionDenied,
		Payload: map[string]any{"tool": "execute_write", "reason": "denied"},
	}
	sig, ok := FailureSignalFromEvent(context.Background(), e)
	if !ok || sig.Code != FailureCodePolicyViolation {
		t.Fatalf("got ok=%v sig=%#v", ok, sig)
	}
}

func TestAttachFailureSignalBridge_RecordsToolFailedAndGuardrail(t *testing.T) {
	bus := events.NewBus()
	rec := &RecordingFailureSink{}
	AttachFailureSignalBridge(bus, rec)

	ctx := context.WithValue(context.Background(), tool.ContextKeyAgentID, "a1")
	ctx = context.WithValue(ctx, tool.ContextKeySessionID, "s1")
	bus.Publish(ctx, events.Event{
		Kind:    events.ToolFailed,
		Payload: map[string]any{"tool": "t", "error": "e"},
	})
	bus.Publish(ctx, events.Event{
		Kind:    events.ToolGuardrailWarn,
		Payload: map[string]any{"rule": "same_tool_failure", "tool": "t", "streak": 2},
	})
	bus.Publish(ctx, events.Event{Kind: events.ToolExecuted, Payload: map[string]any{"tool": "t"}})

	got := rec.Snapshot()
	if len(got) != 2 {
		t.Fatalf("want 2 signals, got %#v", got)
	}
	if got[0].Code != FailureCodeToolFailed || got[1].Code != FailureCodeToolRepeatFail {
		t.Fatalf("codes: %#v", got)
	}
}

func TestAttachFailureSignalBridge_NilSafe(t *testing.T) {
	AttachFailureSignalBridge(nil, &RecordingFailureSink{})
	AttachFailureSignalBridge(events.NewBus(), nil)
}

func TestRingFailureSink_TrimsToN(t *testing.T) {
	r := &RingFailureSink{N: 2}
	r.OnFailureSignal(context.Background(), FailureSignal{Code: "a"})
	r.OnFailureSignal(context.Background(), FailureSignal{Code: "b"})
	r.OnFailureSignal(context.Background(), FailureSignal{Code: "c"})
	got := r.Snapshot()
	if len(got) != 2 || got[0].Code != "b" || got[1].Code != "c" {
		t.Fatalf("got %#v", got)
	}
}
