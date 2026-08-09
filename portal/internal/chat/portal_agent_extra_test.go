package chat

import (
	"testing"

	"github.com/sixath/framework/config"
)

func TestSetPortalAgentExtra_MemoryStoreWriteEnabled(t *testing.T) {
	prev := DefaultHermesP0ToolFlags
	t.Cleanup(func() { SetHermesP0ToolFlags(prev) })
	SetHermesP0ToolFlags(HermesP0ToolFlags{SkillManageConfirmCreateDelete: true})

	SetPortalAgentExtra(&config.PortalAgentExtra{
		MemoryStore: &config.MemoryStoreBlock{
			AgentWorkspace: &config.MemoryStoreAgentWorkspace{WriteEnabled: true},
			Prefetch:       &config.MemoryOrchestratorPrefetch{Enabled: false},
		},
	})
	if !DefaultHermesP0ToolFlags.MemoryWriteEnabled {
		t.Fatal("expected memory_store.agent_workspace.write_enabled to set process flags")
	}
}
