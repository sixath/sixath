package chat

import (
	"context"
	"testing"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
)

func TestProceduralAutoCommit_OnActivate(t *testing.T) {
	store := memory.NewFacade(memory.FacadeConfig{Session: memory.NewSessionMemory()})
	SetPrefetchMemoryStore(store)
	t.Cleanup(func() {
		SetPrefetchMemoryStore(nil)
		SetProceduralRepairConfig(nil)
	})

	SetProceduralRepairConfig(&config.MemoryProceduralRepair{
		Enabled:    true,
		AutoCommit: true,
		MinSupport: 1,
		PilotAgents: []string{"zone-4100-agent"},
		Bindings: []config.MemoryProceduralBindingYAML{{
			TriggerCode: memory.FailureCodeToolFailed,
			ActionKind:  memory.BindingActionSkill,
			SkillID:     "escalation",
		}},
	})

	DefaultFailureSignalSink().OnFailureSignal(context.Background(), memory.FailureSignal{
		Code:      memory.FailureCodeToolFailed,
		AgentID:   "uuid-1",
		AgentName: "zone-4100-agent",
		SessionID: "sess-auto",
	})

	procs, err := store.Recall(context.Background(), memory.RecallQuery{
		Scope: memory.ScopeSession, ScopeID: "sess-auto", Kind: memory.KindProcedural, Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 1 {
		t.Fatalf("want auto-committed procedural unit, got %#v", procs)
	}
}
