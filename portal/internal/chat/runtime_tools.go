package chat

import (
	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/memorysearch"
	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
	"github.com/sixath/framework/tool/browser"
	toolmem "github.com/sixath/framework/tool/memory"
	toolskill "github.com/sixath/framework/tool/skillops"
)

// AgentRuntimeToolsOptions supplies context for RegisterAgentRuntimeTools.
type AgentRuntimeToolsOptions struct {
	Flags           *HermesP0ToolFlags
	SkillsIdx       *skills.Index
	McpServers      []toolskill.McpServerEntry
	AllowScript     bool
	MemoryCfg       *config.MemoryConfig
	MemoryStore     memory.MemoryStore
	SessionProvider memorysearch.SessionTranscriptProvider
	// BrowserStore / BrowserFactory override process defaults (tests inject Fake).
	BrowserStore   *browser.SessionStore
	BrowserFactory func() (browser.Backend, error)
	// VisionAnalyzer enables browser_vision LLM analysis + vision_analyze (optional).
	VisionAnalyzer tool.VisionAnalyzer
}

// RegisterAgentRuntimeTools registers Hermes P0 runtime tools according to flags (spec §14).
func RegisterAgentRuntimeTools(reg *tool.Registry, opts AgentRuntimeToolsOptions) error {
	if reg == nil {
		return nil
	}
	flags := effectiveHermesP0Flags(opts.Flags)
	if flags.SkillManageConfirmCreateDelete != SkillManageConfirmCreateDelete {
		SetSkillManageConfirmCreateDelete(flags.SkillManageConfirmCreateDelete)
	}

	if err := RegisterCoreSkillTools(reg, opts.SkillsIdx, opts.McpServers, opts.AllowScript); err != nil {
		return err
	}
	if flags.SkillRuntimeManageEnabled {
		if err := RegisterSkillRuntimeTools(reg, opts.SkillsIdx, opts.McpServers); err != nil {
			return err
		}
	}

	store := opts.MemoryStore
	if store == nil {
		store = BuildMemoryStore(nil, opts.MemoryCfg, opts.SessionProvider, DefaultMemoryStoreOptions())
	}
	if err := toolmem.RegisterMemoryStoreTools(reg, store, toolmem.StoreToolsOptions{
		AgentWriteEnabled: flags.MemoryWriteEnabled,
	}); err != nil {
		return err
	}

	if flags.TodoEnabled {
		if err := RegisterTodoTools(reg); err != nil {
			return err
		}
	}
	if flags.WorkspaceFilesEnabled {
		if err := RegisterWorkspaceFileTools(reg); err != nil {
			return err
		}
	}
	if flags.WebToolsEnabled {
		if err := registerWebTools(reg, true); err != nil {
			return err
		}
	}
	if flags.TerminalLocalEnabled {
		if err := RegisterTerminalToolsWithEnabled(reg, true); err != nil {
			return err
		}
	}
	if flags.CronjobToolEnabled {
		if err := RegisterCronjobToolsWithEnabled(reg, true); err != nil {
			return err
		}
	}
	if flags.BrowserEnabled {
		if err := RegisterBrowserRuntimeTools(reg, true, opts.BrowserStore, opts.BrowserFactory, opts.VisionAnalyzer); err != nil {
			return err
		}
	}
	return nil
}
