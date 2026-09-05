package chat

import (
	"os"
	"strings"
	"testing"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
)

func TestProceduralBindingGoRemoved(t *testing.T) {
	if _, err := os.Stat("procedural_binding.go"); err == nil {
		t.Fatal("portal procedural catalog wiring must be removed")
	}
}

func TestPortalAgentExtraGo_DoesNotCallSetProceduralRepairConfig(t *testing.T) {
	b, err := os.ReadFile("portal_agent_extra.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "SetProceduralRepairConfig") {
		t.Fatal("SetPortalAgentExtra must ignore MemoryProceduralRepair")
	}
}

func TestBuildPrefetchMemoryOrchestrator_OmitsProceduralBindings(t *testing.T) {
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
