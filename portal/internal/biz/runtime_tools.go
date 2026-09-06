package biz

import agentv1 "backend/api/agent/v1"

// RuntimeToolsConfig is per-agent Hermes P0 opt-in flags (OR-merged with global env).
type RuntimeToolsConfig struct {
	MemoryWriteEnabled        bool  `json:"memory_write_enabled"`
	SkillRuntimeManageEnabled bool  `json:"skill_runtime_manage_enabled"`
	TodoEnabled               bool  `json:"todo_enabled"`
	WorkspaceFilesEnabled     bool  `json:"workspace_files_enabled"`
	WebToolsEnabled           bool  `json:"web_tools_enabled"`
	TerminalLocalEnabled      bool  `json:"terminal_local_enabled"`
	CronjobToolEnabled        bool  `json:"cronjob_tool_enabled"`
	BrowserEnabled            bool  `json:"browser_enabled"`
	HybridRecall              *bool `json:"hybrid_recall,omitempty"` // unset = on; presence preserved end-to-end
}

// RuntimeToolsFromProto maps API proto to biz.
func RuntimeToolsFromProto(p *agentv1.RuntimeToolsConfig) RuntimeToolsConfig {
	if p == nil {
		return RuntimeToolsConfig{}
	}
	cfg := RuntimeToolsConfig{
		MemoryWriteEnabled:        p.GetMemoryWriteEnabled(),
		SkillRuntimeManageEnabled: p.GetSkillRuntimeManageEnabled(),
		TodoEnabled:               p.GetTodoEnabled(),
		WorkspaceFilesEnabled:     p.GetWorkspaceFilesEnabled(),
		WebToolsEnabled:           p.GetWebToolsEnabled(),
		TerminalLocalEnabled:      p.GetTerminalLocalEnabled(),
		CronjobToolEnabled:        p.GetCronjobToolEnabled(),
		BrowserEnabled:            p.GetBrowserEnabled(),
	}
	if p.HybridRecall != nil {
		v := *p.HybridRecall
		cfg.HybridRecall = &v
	}
	return cfg
}

// RuntimeToolsToProto maps biz to API proto.
func RuntimeToolsToProto(c RuntimeToolsConfig) *agentv1.RuntimeToolsConfig {
	out := &agentv1.RuntimeToolsConfig{
		MemoryWriteEnabled:        c.MemoryWriteEnabled,
		SkillRuntimeManageEnabled: c.SkillRuntimeManageEnabled,
		TodoEnabled:               c.TodoEnabled,
		WorkspaceFilesEnabled:     c.WorkspaceFilesEnabled,
		WebToolsEnabled:           c.WebToolsEnabled,
		TerminalLocalEnabled:      c.TerminalLocalEnabled,
		CronjobToolEnabled:        c.CronjobToolEnabled,
		BrowserEnabled:            c.BrowserEnabled,
	}
	if c.HybridRecall != nil {
		v := *c.HybridRecall
		out.HybridRecall = &v
	}
	return out
}
