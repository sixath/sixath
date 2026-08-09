package tool

// DeferConfig 控制各 toolset 是否默认延迟加载（deferred）。
type DeferConfig struct {
	NeverDeferToolsets map[string]bool // default: core
	DeferToolsets      map[string]bool // default: mcp, memory, session_search, web, terminal, cronjob, todo
}

// DefaultDeferConfig 返回内置 defer 策略：core 始终 inline；file/skills inline；其余默认 defer。
func DefaultDeferConfig() DeferConfig {
	return DeferConfig{
		NeverDeferToolsets: map[string]bool{
			ToolsetCore: true,
		},
		DeferToolsets: map[string]bool{
			ToolsetMCP:           true,
			ToolsetMemory:        true,
			ToolsetSessionSearch: true,
			ToolsetWeb:           true,
			ToolsetTerminal:      true,
			ToolsetCronjob:       true,
			ToolsetTodo:          true,
		},
	}
}

// ShouldDefer 判断工具是否应延迟加载。
func ShouldDefer(t Tool, cfg DeferConfig) bool {
	if t.AlwaysLoad {
		return false
	}
	if cfg.NeverDeferToolsets[t.Toolset] {
		return false
	}
	if t.Toolset == ToolsetMCP {
		return true
	}
	return cfg.DeferToolsets[t.Toolset]
}
