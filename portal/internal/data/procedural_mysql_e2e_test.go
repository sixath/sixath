package data

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sixath/framework/memory"
)

// TestProceduralRepair_MySQLBackend_E2E verifies commit + kind filter on SQLite AutoMigrate
// schema that includes the kind column (same GORM model as MySQL).
func TestProceduralRepair_MySQLBackend_E2E(t *testing.T) {
	backend := NewSessionUnitsBackend(openMemoryUnitTestDB(t))
	store := memory.NewFacade(memory.FacadeConfig{Session: backend})

	// Wire portal auto_commit path without importing chat (avoid cycle): call Commit directly.
	b := memory.ProceduralBinding{
		TriggerCode:  memory.FailureCodeToolFailed,
		TriggerQuery: "ssh",
		ActionKind:   memory.BindingActionToolSequence,
		ToolNames:    []string{"ask_user"},
		Mode:         memory.BindingModeSuggest,
	}
	sig := memory.FailureSignal{
		Code:      memory.FailureCodeToolFailed,
		AgentID:   "e8107fb3-e40a-4207-9d9a-6768847aaf79",
		AgentName: "zone-4100-agent",
		SessionID: "mysql-e2e-sess",
	}
	hit, err := store.CommitProceduralRepair(context.Background(), memory.ProceduralCommitInput{
		AgentID:     sig.AgentID,
		AgentName:   sig.AgentName,
		SessionID:   sig.SessionID,
		PilotAgents: []string{"zone-4100-agent"},
		Signal:      sig,
		Binding:     b,
		SupportCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if hit.ID == "" {
		t.Fatal("empty hit id")
	}

	facts, err := store.Recall(context.Background(), memory.RecallQuery{
		Scope: memory.ScopeSession, ScopeID: sig.SessionID, Source: memory.SourceUnits, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("fact recall leaked procedural: %#v", facts)
	}
	procs, err := store.Recall(context.Background(), memory.RecallQuery{
		Scope: memory.ScopeSession, ScopeID: sig.SessionID, Source: memory.SourceUnits,
		Kind: memory.KindProcedural, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 1 {
		t.Fatalf("want 1 procedural, got %#v", procs)
	}
	if procs[0].Metadata["kind"] != memory.KindProcedural {
		t.Fatalf("metadata kind=%v", procs[0].Metadata["kind"])
	}

	_, err = store.Remember(context.Background(), memory.RememberInput{
		Scope: memory.ScopeSession, ScopeID: sig.SessionID, Action: memory.ActionAdd,
		Content: "x", Metadata: map[string]any{"kind": memory.KindProcedural},
	})
	if !errors.Is(err, memory.ErrProceduralRememberBlocked) {
		t.Fatalf("want blocked, got %v", err)
	}

	// Prefetch loads persisted
	pf := &memory.StorePrefetchBackend{
		Store: store, MaxSnippets: 5, MaxProcedural: 3,
		LoadPersistedProcedural: true,
		ProceduralBindings:      []memory.ProceduralBinding{b},
	}
	parts, err := pf.Prefetch(context.Background(), memory.PrefetchQuery{
		UserMessage: "retry ssh please",
		AgentID:     sig.AgentID,
		SessionID:   sig.SessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range parts {
		if p.Label == "procedural" && strings.Contains(p.Content, "ask_user") {
			found = true
		}
	}
	if !found {
		t.Fatalf("prefetch missing procedural: %#v", parts)
	}
}
