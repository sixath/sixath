package chat_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"backend/internal/chat"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/events"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/tool"
)

// TestProceduralRepair_E2E walks the full P3-A…E path in-process:
// FailureSignal → catalog activate → auto_commit → fact-only recall exclude →
// procedural recall + Prefetch inject (trigger_query + persisted).
func TestProceduralRepair_E2E(t *testing.T) {
	store := memory.NewFacade(memory.FacadeConfig{Session: memory.NewSessionMemory()})
	chat.SetPrefetchMemoryStore(store)
	t.Cleanup(func() {
		chat.SetPrefetchMemoryStore(nil)
		chat.SetProceduralRepairConfig(nil)
	})

	prefetchOn := true
	chat.SetProceduralRepairConfig(&config.MemoryProceduralRepair{
		Enabled:       true,
		AutoCommit:    true,
		MinSupport:    2,
		MaxProcedural: 3,
		PilotAgents:   []string{"zone-4100-agent"},
		Inject:        &config.MemoryProceduralInject{Prefetch: &prefetchOn},
		Bindings: []config.MemoryProceduralBindingYAML{{
			TriggerCode:  memory.FailureCodeToolFailed,
			TriggerQuery: "ssh",
			ActionKind:   memory.BindingActionToolSequence,
			ToolNames:    []string{"ask_user"},
			Mode:         memory.BindingModeSuggest,
		}},
	})

	sessionID := "e2e-proc-sess"
	agentID := "agent-uuid-e2e"
	agentName := "zone-4100-agent"

	turnBus := events.NewBus()
	epBuf := memory.NewEpisodeLocalBuffer(sessionID)
	memory.AttachFailureSignalBridge(turnBus, memory.MultiFailureSink{
		chat.DefaultFailureSignalSink(),
		memory.EpisodeLocalFailureSink{Buffer: epBuf},
	})

	ctx := context.Background()
	ctx = context.WithValue(ctx, tool.ContextKeyAgentID, agentID)
	ctx = context.WithValue(ctx, tool.ContextKeyAgentName, agentName)
	ctx = context.WithValue(ctx, tool.ContextKeySessionID, sessionID)

	// First failure → candidate only (min_support=2)
	turnBus.Publish(ctx, events.Event{
		Kind:    events.ToolFailed,
		Payload: map[string]any{"tool": "ssh_exec", "error": "connection refused"},
	})
	if n := countProcedural(t, store, sessionID); n != 0 {
		t.Fatalf("after 1 signal want 0 committed, got %d", n)
	}
	if len(epBuf.Signals()) != 1 {
		t.Fatalf("episode buffer want 1 signal, got %d", len(epBuf.Signals()))
	}

	// Second failure → activate + auto_commit
	turnBus.Publish(ctx, events.Event{
		Kind:    events.ToolFailed,
		Payload: map[string]any{"tool": "ssh_exec", "error": "connection refused again"},
	})
	if n := countProcedural(t, store, sessionID); n != 1 {
		t.Fatalf("after 2 signals want 1 committed, got %d", n)
	}

	// Remember bare procedural still blocked
	_, err := store.Remember(ctx, memory.RememberInput{
		Scope: memory.ScopeSession, ScopeID: sessionID, Action: memory.ActionAdd, Content: "hack",
		Metadata: map[string]any{"kind": memory.KindProcedural},
	})
	if !errors.Is(err, memory.ErrProceduralRememberBlocked) {
		t.Fatalf("Remember bare procedural should block, got %v", err)
	}

	// Fact-only recall excludes procedural
	facts, err := store.Recall(ctx, memory.RecallQuery{
		Scope: memory.ScopeSession, ScopeID: sessionID, Source: memory.SourceUnits, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("fact recall must exclude procedural, got %#v", facts)
	}

	// Explicit procedural lane
	procs, err := store.Recall(ctx, memory.RecallQuery{
		Scope: memory.ScopeSession, ScopeID: sessionID, Source: memory.SourceUnits,
		Kind: memory.KindProcedural, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 1 {
		t.Fatalf("procedural lane want 1, got %#v", procs)
	}
	if memory.UnitKindFromMetadata(procs[0].Metadata) != memory.KindProcedural {
		t.Fatalf("kind=%v", procs[0].Metadata["kind"])
	}
	if !strings.Contains(procs[0].Content, "ask_user") {
		t.Fatalf("content should mention ask_user: %s", procs[0].Content)
	}

	// Prefetch: catalog active + persisted units → procedural label
	b := &memory.StorePrefetchBackend{
		Store:                   store,
		MaxSnippets:             5,
		ProceduralBindings:      mustActiveBindings(t),
		MaxProcedural:           3,
		LoadPersistedProcedural: true,
	}
	parts, err := b.Prefetch(ctx, memory.PrefetchQuery{
		UserMessage: "please fix ssh after tool failure",
		AgentID:     agentID,
		SessionID:   sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var procParts int
	var joined string
	for _, p := range parts {
		if p.Label == "procedural" {
			procParts++
			joined += p.Content + "\n"
		}
	}
	if procParts == 0 || !strings.Contains(joined, "ask_user") {
		t.Fatalf("want procedural prefetch hint with ask_user, parts=%#v", parts)
	}

	// Episode clear does not remove committed unit
	epBuf.Clear()
	if len(epBuf.Signals()) != 0 {
		t.Fatal("episode clear failed")
	}
	if n := countProcedural(t, store, sessionID); n != 1 {
		t.Fatalf("committed unit must survive episode clear, got %d", n)
	}

	// Non-pilot agent must not auto_commit
	chat.SetProceduralRepairConfig(&config.MemoryProceduralRepair{
		Enabled: true, AutoCommit: true, MinSupport: 1,
		PilotAgents: []string{"zone-4100-agent"},
		Bindings: []config.MemoryProceduralBindingYAML{{
			TriggerCode: memory.FailureCodeToolFailed,
			ActionKind:  memory.BindingActionToolSequence,
			ToolNames:   []string{"ask_user"},
		}},
	})
	chat.SetPrefetchMemoryStore(store)
	otherSess := "e2e-other"
	turnBus2 := events.NewBus()
	memory.AttachFailureSignalBridge(turnBus2, chat.DefaultFailureSignalSink())
	ctx2 := context.WithValue(context.Background(), tool.ContextKeyAgentID, "other-uuid")
	ctx2 = context.WithValue(ctx2, tool.ContextKeyAgentName, "not-pilot")
	ctx2 = context.WithValue(ctx2, tool.ContextKeySessionID, otherSess)
	turnBus2.Publish(ctx2, events.Event{
		Kind: events.ToolFailed, Payload: map[string]any{"tool": "x", "error": "e"},
	})
	if n := countProcedural(t, store, otherSess); n != 0 {
		t.Fatalf("non-pilot must not commit, got %d", n)
	}
}

func mustActiveBindings(t *testing.T) []memory.ProceduralBinding {
	t.Helper()
	binds, _, _ := chat.ProceduralBindingsForPrefetch()
	if len(binds) == 0 {
		t.Fatal("expected active catalog bindings after activate")
	}
	return binds
}

func countProcedural(t *testing.T, store memory.MemoryStore, sessionID string) int {
	t.Helper()
	hits, err := store.Recall(context.Background(), memory.RecallQuery{
		Scope: memory.ScopeSession, ScopeID: sessionID, Source: memory.SourceUnits,
		Kind: memory.KindProcedural, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	return len(hits)
}
