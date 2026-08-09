package chat

import (
	"github.com/sixath/framework/tool"
)

// sharedProcessRegistry is process-local; one registry per portal process for terminal+process tools.
var sharedProcessRegistry = tool.NewProcessRegistry()

// RegisterTerminalTools registers local terminal when enabled via env or global flag.
func RegisterTerminalTools(reg *tool.Registry) error {
	return RegisterTerminalToolsWithEnabled(reg, tool.TerminalLocalEnabled || tool.TerminalEnabledFromEnv())
}

// RegisterTerminalToolsWithEnabled registers terminal + process with an explicit enabled flag.
func RegisterTerminalToolsWithEnabled(reg *tool.Registry, enabled bool) error {
	if reg == nil || !enabled {
		return nil
	}
	cfg := &tool.TerminalConfig{
		Enabled:      true,
		PendingStore: tool.NewInMemoryTerminalPendingStore(),
		TokenGen:     tool.RandomTokenGenerator{},
		Processes:    sharedProcessRegistry,
	}
	if err := tool.RegisterTerminalTool(reg, cfg); err != nil {
		return err
	}
	return tool.RegisterProcessTool(reg, sharedProcessRegistry, true)
}

// ProcessRegistryForHooks returns the shared registry for ChatSession end cleanup.
func ProcessRegistryForHooks() *tool.ProcessRegistry {
	return sharedProcessRegistry
}
