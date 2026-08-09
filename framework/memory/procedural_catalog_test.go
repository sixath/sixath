package memory

import (
	"context"
	"testing"

	"github.com/sixath/framework/events"
)

func TestProceduralCatalog_StrengthenThenActive(t *testing.T) {
	cat := NewProceduralCatalog(2, nil)
	b := ProceduralBinding{
		TriggerCode: FailureCodeToolRepeatFail,
		ActionKind:  BindingActionSkill,
		SkillID:     "fix",
		AgentID:     "zone-4100-agent",
	}
	cat.SeedBindings([]ProceduralBinding{b})
	if n := len(cat.ActiveBindings()); n != 0 {
		t.Fatalf("candidate must not be active yet, got %d", n)
	}
	cat.ObserveSignal(FailureSignal{Code: FailureCodeToolRepeatFail, AgentID: "zone-4100-agent"})
	if n := len(cat.ActiveBindings()); n != 0 {
		t.Fatalf("after 1 signal still candidate, got %d", n)
	}
	cat.ObserveSignal(FailureSignal{Code: FailureCodeToolRepeatFail, AgentID: "zone-4100-agent"})
	act := cat.ActiveBindings()
	if len(act) != 1 || act[0].SkillID != "fix" {
		t.Fatalf("want active after 2, got %#v", act)
	}
}

func TestProceduralCatalog_QueryOnlyStartsActive(t *testing.T) {
	cat := NewProceduralCatalog(2, nil)
	cat.SeedBindings([]ProceduralBinding{{
		TriggerQuery: "转人工",
		ActionKind:   BindingActionSkill,
		SkillID:      "escalation",
	}})
	if n := len(cat.ActiveBindings()); n != 1 {
		t.Fatalf("query-only should be active, got %d", n)
	}
}

func TestProceduralCatalog_DisableStopsActive(t *testing.T) {
	cat := NewProceduralCatalog(1, nil)
	b := ProceduralBinding{
		TriggerCode: FailureCodeToolFailed,
		ActionKind:  BindingActionToolSequence,
		ToolNames:   []string{"ssh_exec"},
	}
	cat.SeedBindings([]ProceduralBinding{b})
	cat.ObserveSignal(FailureSignal{Code: FailureCodeToolFailed})
	if len(cat.ActiveBindings()) != 1 {
		t.Fatal("expected active")
	}
	id := EntryIDForBinding(b)
	if !cat.Disable(id) {
		t.Fatal("disable id")
	}
	if n := len(cat.ActiveBindings()); n != 0 {
		t.Fatalf("disabled still active: %d", n)
	}
}

func TestProceduralCatalog_DisableByCode(t *testing.T) {
	cat := NewProceduralCatalog(1, nil)
	cat.SeedBindings([]ProceduralBinding{{
		TriggerCode: FailureCodeToolRepeatFail,
		ActionKind:  BindingActionSkill,
		SkillID:     "a",
		AgentID:     "ag1",
	}})
	cat.ObserveSignal(FailureSignal{Code: FailureCodeToolRepeatFail, AgentID: "ag1"})
	if cat.DisableByCode("ag1", FailureCodeToolRepeatFail) != 1 {
		t.Fatal("disable by code")
	}
	if len(cat.ActiveBindings()) != 0 {
		t.Fatal("still active")
	}
}

func TestProceduralCatalog_RecordHit(t *testing.T) {
	cat := NewProceduralCatalog(1, nil)
	b := ProceduralBinding{TriggerQuery: "x", ActionKind: BindingActionSkill, SkillID: "s"}
	cat.SeedBindings([]ProceduralBinding{b})
	matched := MatchProceduralBindings(cat.ActiveBindings(), "", "hello x world", nil)
	cat.RecordHit(ProceduralHitPrefetch, matched)
	snap := cat.Snapshot()
	if len(snap) != 1 || snap[0].PrefetchHits != 1 {
		t.Fatalf("hits: %#v", snap)
	}
}

func TestProceduralCatalogSink_ViaBridge(t *testing.T) {
	cat := NewProceduralCatalog(2, nil)
	cat.SeedBindings([]ProceduralBinding{{
		TriggerCode: FailureCodeToolFailed,
		ActionKind:  BindingActionSkill,
		SkillID:     "s",
	}})
	bus := events.NewBus()
	AttachFailureSignalBridge(bus, ProceduralCatalogSink{Catalog: cat})
	bus.Publish(context.Background(), events.Event{
		Kind:    events.ToolFailed,
		Payload: map[string]any{"tool": "t", "error": "e"},
	})
	bus.Publish(context.Background(), events.Event{
		Kind:    events.ToolFailed,
		Payload: map[string]any{"tool": "t", "error": "e"},
	})
	if len(cat.ActiveBindings()) != 1 {
		t.Fatalf("want active after 2 tool_failed, snap=%#v", cat.Snapshot())
	}
}
