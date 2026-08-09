package service

import (
	"context"
	"testing"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/tool"
)

func TestAttachFailureSignalOnTurnBus(t *testing.T) {
	turnBus := events.NewBus()
	rec := &memory.RecordingFailureSink{}
	memory.AttachFailureSignalBridge(turnBus, rec)

	ctx := context.WithValue(context.Background(), tool.ContextKeyAgentID, "zone-4100-agent")
	ctx = context.WithValue(ctx, tool.ContextKeySessionID, "sess-1")
	turnBus.Publish(ctx, events.Event{
		Kind:    events.ToolFailed,
		Payload: map[string]any{"tool": "ssh_exec", "error": "boom"},
	})
	got := rec.Snapshot()
	if len(got) != 1 || got[0].Code != memory.FailureCodeToolFailed {
		t.Fatalf("got %#v", got)
	}
	if got[0].AgentID != "zone-4100-agent" || got[0].SessionID != "sess-1" {
		t.Fatalf("identity: %#v", got[0])
	}
}
