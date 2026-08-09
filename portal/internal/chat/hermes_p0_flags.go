package chat

import (
	"log"
	"os"
	"strings"
	"sync"

	"backend/internal/biz"
)

// HermesP0ToolFlags controls opt-in registration of Hermes P0 runtime tools (spec §14 / NFR-1).
// All fields default to false so production behavior is unchanged until explicitly enabled.
type HermesP0ToolFlags struct {
	MemoryWriteEnabled             bool
	SkillRuntimeManageEnabled      bool
	SkillManageConfirmCreateDelete bool
	TodoEnabled                    bool
	WorkspaceFilesEnabled          bool
	WebToolsEnabled                bool
	TerminalLocalEnabled           bool
	CronjobToolEnabled             bool
	BrowserEnabled                 bool
}

var browserEnabledDeprecateOnce sync.Once

// DefaultHermesP0ToolFlags is the process-wide default (all false).
var DefaultHermesP0ToolFlags = HermesP0ToolFlags{
	SkillManageConfirmCreateDelete: true,
}

// SetHermesP0ToolFlags replaces the process-wide default flags (e.g. after loading YAML).
func SetHermesP0ToolFlags(f HermesP0ToolFlags) {
	DefaultHermesP0ToolFlags = f
}

// EnrichHermesP0FromEnv merges truthy env vars into flags (does not disable YAML-true fields).
func EnrichHermesP0FromEnv(f *HermesP0ToolFlags) {
	if f == nil {
		return
	}
	if envTruthy("SATH_AGENT_MEMORY_WRITE_ENABLED") {
		f.MemoryWriteEnabled = true
	}
	if envTruthy("SATH_SKILL_RUNTIME_MANAGE_ENABLED") {
		f.SkillRuntimeManageEnabled = true
	}
	if v := strings.TrimSpace(os.Getenv("SATH_SKILL_MANAGE_CONFIRM_CREATE_DELETE")); v != "" {
		f.SkillManageConfirmCreateDelete = envTruthyValue(v)
	}
	if envTruthy("SATH_TODO_ENABLED") {
		f.TodoEnabled = true
	}
	if envTruthy("SATH_WORKSPACE_FILES_ENABLED") {
		f.WorkspaceFilesEnabled = true
	}
	if envTruthy("SATH_WEB_TOOLS_ENABLED") {
		f.WebToolsEnabled = true
	}
	if envTruthy("TERMINAL_LOCAL_ENABLED") {
		f.TerminalLocalEnabled = true
	}
	if envTruthy("CRONJOB_TOOL_ENABLED") {
		f.CronjobToolEnabled = true
	}
	if envTruthy("SATH_BROWSER_ENABLED") {
		f.BrowserEnabled = true
	} else if envTruthy("BROWSER_ENABLED") {
		f.BrowserEnabled = true
		browserEnabledDeprecateOnce.Do(func() {
			log.Printf("BROWSER_ENABLED is deprecated; use SATH_BROWSER_ENABLED")
		})
	}
}

func envTruthy(key string) bool {
	return envTruthyValue(strings.TrimSpace(os.Getenv(key)))
}

func envTruthyValue(v string) bool {
	v = strings.ToLower(v)
	return v == "1" || v == "true" || v == "yes"
}

func effectiveHermesP0Flags(override *HermesP0ToolFlags) HermesP0ToolFlags {
	if override != nil {
		return *override
	}
	return DefaultHermesP0ToolFlags
}

// MergeHermesP0Flags OR-merges agent-level flags into the global/default flags.
func MergeHermesP0Flags(global HermesP0ToolFlags, agent HermesP0ToolFlags) HermesP0ToolFlags {
	return HermesP0ToolFlags{
		MemoryWriteEnabled:             global.MemoryWriteEnabled || agent.MemoryWriteEnabled,
		SkillRuntimeManageEnabled:      global.SkillRuntimeManageEnabled || agent.SkillRuntimeManageEnabled,
		SkillManageConfirmCreateDelete: global.SkillManageConfirmCreateDelete || agent.SkillManageConfirmCreateDelete,
		TodoEnabled:                    global.TodoEnabled || agent.TodoEnabled,
		WorkspaceFilesEnabled:          global.WorkspaceFilesEnabled || agent.WorkspaceFilesEnabled,
		WebToolsEnabled:                global.WebToolsEnabled || agent.WebToolsEnabled,
		TerminalLocalEnabled:           global.TerminalLocalEnabled || agent.TerminalLocalEnabled,
		CronjobToolEnabled:             global.CronjobToolEnabled || agent.CronjobToolEnabled,
		BrowserEnabled:                 global.BrowserEnabled || agent.BrowserEnabled,
	}
}

// HermesP0FlagsFromRuntimeTools maps persisted agent runtime_tools to Hermes flags.
func HermesP0FlagsFromRuntimeTools(c biz.RuntimeToolsConfig) HermesP0ToolFlags {
	return HermesP0ToolFlags{
		MemoryWriteEnabled:        c.MemoryWriteEnabled,
		SkillRuntimeManageEnabled: c.SkillRuntimeManageEnabled,
		TodoEnabled:               c.TodoEnabled,
		WorkspaceFilesEnabled:     c.WorkspaceFilesEnabled,
		WebToolsEnabled:           c.WebToolsEnabled,
		TerminalLocalEnabled:      c.TerminalLocalEnabled,
		CronjobToolEnabled:        c.CronjobToolEnabled,
		BrowserEnabled:            c.BrowserEnabled,
	}
}

// RuntimeToolsForAgent returns global env/default flags OR-merged with the agent's saved runtime_tools.
// Web tools are fail-closed on the agent flag: webToolsEnabled=false never inherits process/env enablement
// (see portal#7 — Bocha key / SATH_WEB_TOOLS_ENABLED must not override an explicit agent disable).
func RuntimeToolsForAgent(agent *biz.AgentMeta) HermesP0ToolFlags {
	global := DefaultHermesP0ToolFlags
	if agent == nil {
		return global
	}
	merged := MergeHermesP0Flags(global, HermesP0FlagsFromRuntimeTools(agent.RuntimeTools))
	if !agent.RuntimeTools.WebToolsEnabled {
		merged.WebToolsEnabled = false
	}
	return merged
}

// HermesP0FlagsPtrForAgent is a convenience pointer for RegisterAgentRuntimeTools Options.Flags.
func HermesP0FlagsPtrForAgent(agent *biz.AgentMeta) *HermesP0ToolFlags {
	f := RuntimeToolsForAgent(agent)
	return &f
}
