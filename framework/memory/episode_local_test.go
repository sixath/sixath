package memory

import (
	"context"
	"testing"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/tool"
)

func TestEpisodeLocalBuffer_ClearDropsSignals(t *testing.T) {
	b := NewEpisodeLocalBuffer("sess-1")
	b.PutSignal(FailureSignal{Code: FailureCodeToolFailed, Message: "x"})
	b.PutNote("retry tip")
	if len(b.Signals()) != 1 || len(b.Notes()) != 1 {
		t.Fatalf("want 1 signal and 1 note")
	}
	b.Clear()
	if len(b.Signals()) != 0 || len(b.Notes()) != 0 {
		t.Fatalf("after Clear got signals=%d notes=%d", len(b.Signals()), len(b.Notes()))
	}
}

func TestEpisodeLocalBuffer_NotMemoryStore(t *testing.T) {
	// Episode-local content must not appear via MemoryStore Recall.
	store := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	b := NewEpisodeLocalBuffer("sess-ep")
	b.PutSignal(FailureSignal{Code: FailureCodeToolFailed, ToolName: "ssh_exec", Message: "boom"})
	hits, err := store.Recall(context.Background(), RecallQuery{
		Scope:   ScopeSession,
		ScopeID: "sess-ep",
		Source:  SourceUnits,
		Query:   "boom",
		Limit:   5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("episode-local must not be in MemoryStore, got %#v", hits)
	}
	b.Clear()
}

func TestEpisodeLocalFailureSink_ViaBridge(t *testing.T) {
	bus := events.NewBus()
	buf := NewEpisodeLocalBuffer("s1")
	AttachFailureSignalBridge(bus, EpisodeLocalFailureSink{Buffer: buf})

	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "s1")
	bus.Publish(ctx, events.Event{
		Kind:    events.ToolFailed,
		Payload: map[string]any{"tool": "t", "error": "e"},
	})
	if n := len(buf.Signals()); n != 1 {
		t.Fatalf("want 1, got %d", n)
	}
	buf.Clear()
	if n := len(buf.Signals()); n != 0 {
		t.Fatalf("cleared want 0, got %d", n)
	}
}
