package memory

import (
	"context"
	"errors"
	"testing"
)

func TestCommitProceduralRepair_Gates(t *testing.T) {
	store := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	b := ProceduralBinding{
		TriggerCode: FailureCodeToolFailed,
		ActionKind:  BindingActionSkill,
		SkillID:     "escalation",
		Mode:        BindingModeSuggest,
	}
	sig := FailureSignal{Code: FailureCodeToolFailed, AgentID: "uuid-1", AgentName: "zone-4100-agent", SessionID: "sess-1"}

	_, err := store.CommitProceduralRepair(context.Background(), ProceduralCommitInput{
		AgentID: "uuid-1", AgentName: "other", PilotAgents: []string{"zone-4100-agent"},
		Signal: sig, Binding: b,
	})
	if !errors.Is(err, ErrProceduralCommitRejected) {
		t.Fatalf("want pilot reject, got %v", err)
	}

	hit, err := store.CommitProceduralRepair(context.Background(), ProceduralCommitInput{
		AgentID: "uuid-1", AgentName: "zone-4100-agent", PilotAgents: []string{"zone-4100-agent"},
		Signal: sig, Binding: b, SupportCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if hit.ID == "" {
		t.Fatal("expected unit id")
	}
	if UnitKindFromMetadata(hit.Metadata) != KindProcedural && hit.Metadata["kind"] != KindProcedural {
		// session hit may not echo metadata.kind if Remember clones — check recall
	}

	facts, err := store.Recall(context.Background(), RecallQuery{Scope: ScopeSession, ScopeID: "sess-1", Source: SourceUnits, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("default recall must exclude procedural, got %#v", facts)
	}
	procs, err := store.Recall(context.Background(), RecallQuery{
		Scope: ScopeSession, ScopeID: "sess-1", Source: SourceUnits, Kind: KindProcedural, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 1 {
		t.Fatalf("want 1 procedural, got %#v", procs)
	}

	_, err = store.Remember(context.Background(), RememberInput{
		Scope: ScopeSession, ScopeID: "sess-1", Action: ActionAdd, Content: "x",
		Metadata: map[string]any{"kind": KindProcedural},
	})
	if !errors.Is(err, ErrProceduralRememberBlocked) {
		t.Fatalf("Remember still blocks bare procedural: %v", err)
	}
}

func TestCommitProceduralRepair_MergeSameContent(t *testing.T) {
	store := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	b := ProceduralBinding{TriggerCode: FailureCodeToolFailed, ActionKind: BindingActionSkill, SkillID: "esc"}
	sig := FailureSignal{Code: FailureCodeToolFailed, AgentName: "zone-4100-agent", SessionID: "s"}
	in := ProceduralCommitInput{
		AgentName: "zone-4100-agent", PilotAgents: []string{"zone-4100-agent"},
		Signal: sig, Binding: b,
	}
	if _, err := store.CommitProceduralRepair(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	hit, err := store.CommitProceduralRepair(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if hit.ID != "" {
		t.Fatalf("merge should no-op, got id %s", hit.ID)
	}
	procs, _ := store.List(context.Background(), ListFilter{Scope: ScopeSession, ScopeID: "s", Kind: KindProcedural})
	if len(procs) != 1 {
		t.Fatalf("want 1 after merge, got %d", len(procs))
	}
}

func TestSessionMemory_KindFilter(t *testing.T) {
	m := NewSessionMemory()
	ctx := context.Background()
	_, _ = m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "fact-a",
		Metadata: map[string]any{"kind": KindFact},
	})
	_, _ = m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "proc-a",
		Metadata: map[string]any{"kind": KindProcedural},
	})
	facts, err := m.Recall(ctx, RecallQuery{Scope: ScopeSession, ScopeID: "s", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].Content != "fact-a" {
		t.Fatalf("fact-only: %#v", facts)
	}
	procs, _ := m.Recall(ctx, RecallQuery{Scope: ScopeSession, ScopeID: "s", Kind: KindProcedural, Limit: 10})
	if len(procs) != 1 {
		t.Fatalf("procedural: %#v", procs)
	}
}
