package chat

import (
	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
	toolskill "github.com/sixath/framework/tool/skillops"
)

// RegisterCoreSkillTools registers load_skill, read_skill_file, execute_skill_script (baseline).
func RegisterCoreSkillTools(reg *tool.Registry, idx *skills.Index, mcpServers []toolskill.McpServerEntry, allowScript bool) error {
	if idx == nil {
		return nil
	}
	if err := toolskill.RegisterLoadSkillTool(reg, idx, mcpServers); err != nil {
		return err
	}
	allow := allowScript && AllowScriptExecution
	return toolskill.RegisterExecuteSkillScriptTool(reg, idx, allow, nil)
}

// RegisterSkillRuntimeTools registers Hermes skills_list, skill_view, skill_manage (opt-in).
// skill_manage uses SkillManageToolConfig（主对话：含 patch confirm）。
func RegisterSkillRuntimeTools(reg *tool.Registry, idx *skills.Index, mcpServers []toolskill.McpServerEntry) error {
	return RegisterSkillRuntimeToolsWithManage(reg, idx, mcpServers, SkillManageToolConfig(idx))
}

// RegisterSkillRuntimeToolsWithManage 允许调用方覆盖 skill_manage 配置（Growth fork 关闭 patch confirm）。
func RegisterSkillRuntimeToolsWithManage(reg *tool.Registry, idx *skills.Index, mcpServers []toolskill.McpServerEntry, manageCfg *toolskill.SkillManageConfig) error {
	if idx == nil {
		return nil
	}
	if err := toolskill.RegisterSkillsListViewTools(reg, idx, mcpServers); err != nil {
		return err
	}
	return toolskill.RegisterSkillManageTool(reg, manageCfg)
}

// RegisterSkillTools registers core skill tools only (legacy helper for ExecuteSkillScript).
func RegisterSkillTools(reg *tool.Registry, idx *skills.Index, mcpServers []toolskill.McpServerEntry, allowScript bool) error {
	return RegisterCoreSkillTools(reg, idx, mcpServers, allowScript)
}
