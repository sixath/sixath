package harness

import "github.com/sixath/framework/config"

// ToolGuardrailsFromConfig 将 framework/config 中的 YAML 形状转为 ReAct 运行时配置；c 为 nil 或未启用时返回 nil。
func ToolGuardrailsFromConfig(c *config.ToolGuardrails) *ToolGuardrailsConfig {
	if c == nil || !c.Enabled {
		return nil
	}
	warningsOnly := c.WarningsOnly
	if c.SameArgsFailureHalt == 0 && c.SameToolFailureHalt == 0 {
		warningsOnly = true
	}
	out := ToolGuardrailsConfig{
		Enabled:                   true,
		HardHalt:                  !warningsOnly,
		SameArgsFailureWarn:       c.SameArgsFailureWarn,
		SameArgsFailureHalt:       c.SameArgsFailureHalt,
		SameToolFailureWarn:       c.SameToolFailureWarn,
		SameToolFailureHalt:       c.SameToolFailureHalt,
		IdempotentRelaxMultiplier: c.IdempotentRelaxMultiplier,
		NoProgressToolOnlyWarn:    c.NoProgressToolOnlyWarn,
		NoProgressToolOnlyHalt:    c.NoProgressToolOnlyHalt,
	}
	if len(c.IdempotentTools) > 0 {
		out.IdempotentTools = append([]string(nil), c.IdempotentTools...)
	}
	if len(c.MutatingTools) > 0 {
		out.MutatingTools = append([]string(nil), c.MutatingTools...)
	}
	return &out
}
