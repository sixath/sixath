package chat

import (
	"os"
	"strings"
	"testing"
)

func TestShelfGo_toolFamiliesAndCodeModelRemoved(t *testing.T) {
	for _, name := range []string{"tool_families.go", "code_model.go"} {
		if _, err := os.Stat(name); err == nil {
			t.Errorf("%s must not exist", name)
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

func TestShelfGo_doesNotKeepZeroCallerHooks(t *testing.T) {
	checks := []struct {
		file   string
		needle string
	}{
		{"agent_builder.go", "RegisterLearningTools"},
		{"memory_prefetch_bootstrap.go", "prefetchOrchestratorForReAct"},
		{"memory_extract.go", "NotifyMemoryExtractFromTurn"},
		{"memory_graph.go", "NotifyMemoryGraphFromTurn"},
		{"portal_agent_extra.go", "RebuildPrefetchMemoryOrchestrator"},
	}
	for _, c := range checks {
		b, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), c.needle) {
			t.Errorf("%s must not contain %q", c.file, c.needle)
		}
	}
}
