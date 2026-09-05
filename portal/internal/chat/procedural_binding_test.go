package chat

import (
	"testing"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
)

func TestDisableProceduralEntry_AfterSeed(t *testing.T) {
	SetProceduralRepairConfig(&config.MemoryProceduralRepair{
		Enabled: true,
		Bindings: []config.MemoryProceduralBindingYAML{{
			TriggerQuery: "转人工",
			ActionKind:   "skill",
			SkillID:      "escalation",
			Mode:         "suggest",
		}},
	})
	t.Cleanup(func() { SetProceduralRepairConfig(nil) })

	id := memory.EntryIDForBinding(memory.ProceduralBinding{
		TriggerQuery: "转人工",
		ActionKind:   memory.BindingActionSkill,
		SkillID:      "escalation",
	})
	if !DisableProceduralEntry(id) {
		t.Fatal("disable seeded entry")
	}
	if DisableProceduralEntry("missing-entry-id") {
		t.Fatal("unknown id must not disable")
	}
}

func TestBuildPrefetchMemoryOrchestrator_OmitsProceduralBindings(t *testing.T) {
	SetProceduralRepairConfig(&config.MemoryProceduralRepair{
		Enabled: true,
		Bindings: []config.MemoryProceduralBindingYAML{{
			TriggerQuery: "转人工",
			ActionKind:   "skill",
			SkillID:      "escalation",
		}},
	})
	t.Cleanup(func() { SetProceduralRepairConfig(nil) })

	o := BuildPrefetchMemoryOrchestrator(&config.MemoryOrchestratorPrefetch{Enabled: true, MaxSnippets: 3})
	if o == nil || len(o.Backends) != 1 {
		t.Fatalf("orchestrator backends=%v", o)
	}
	b, ok := o.Backends[0].(*memory.StorePrefetchBackend)
	if !ok {
		t.Fatalf("backend type %T", o.Backends[0])
	}
	if len(b.ProceduralBindings) != 0 || b.LoadPersistedProcedural {
		t.Fatalf("default prefetch must not inject procedural: binds=%d loadPersisted=%v", len(b.ProceduralBindings), b.LoadPersistedProcedural)
	}
}
