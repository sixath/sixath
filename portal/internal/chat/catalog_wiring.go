package chat

import (
	"context"
	"strings"

	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
)

// CatalogWiringInput 构建 Agent 工具目录所需的运行时上下文。
type CatalogWiringInput struct {
	Reg            *tool.Registry
	DsBindings     []DatasourceBinding
	WecomChannelID string
	ChannelType    string // "wecom" when bound
	SkillsIdx      *skills.Index
}

// BuildCatalogForAgent 按 Agent 绑定装配 CatalogProvider 并生成工具目录快照。
func BuildCatalogForAgent(ctx context.Context, in CatalogWiringInput) tool.ToolCatalog {
	providers := []tool.CatalogProvider{}
	if len(in.DsBindings) > 0 {
		providers = append(providers, &DatasourceCatalogProvider{Bindings: in.DsBindings})
	}
	if in.WecomChannelID != "" {
		providers = append(providers, &ChannelCatalogProvider{ChannelID: in.WecomChannelID, ChannelType: in.ChannelType})
	}
	if in.SkillsIdx != nil {
		providers = append(providers, &SkillsCatalogProvider{Index: in.SkillsIdx})
	}
	providers = append(providers, &WebToolsCatalogProvider{}, &tool.McpCatalogProvider{})
	return tool.BuildToolCatalog(ctx, in.Reg, providers...)
}

// RegisterCatalogTools 注册 list_tools 等目录发现工具。
func RegisterCatalogTools(reg *tool.Registry) error {
	return tool.RegisterListToolsTool(reg, nil)
}

// WireCatalogAndToolSearch 注册目录工具、构建 catalog，并在满足条件时启用 tool_search 桥接。
func WireCatalogAndToolSearch(ctx context.Context, in CatalogWiringInput) (tool.ToolCatalog, bool, error) {
	if err := RegisterCatalogTools(in.Reg); err != nil {
		return tool.ToolCatalog{}, false, err
	}
	catalog := BuildCatalogForAgent(ctx, in)
	active, err := RegisterToolSearchIfNeeded(ctx, in.Reg, catalog)
	if err != nil {
		return catalog, false, err
	}
	if active {
		catalog = BuildCatalogForAgent(ctx, in) // rebuild after bridge tools registered
	}
	return catalog, active, nil
}

// AppendToolCatalogPrompt 将工具目录块置顶拼接到 base system prompt 之前。
func AppendToolCatalogPrompt(base string, catalog tool.ToolCatalog) string {
	p := FormatToolCatalogPrompt(catalog)
	if p == "" {
		return base
	}
	if strings.TrimSpace(base) == "" {
		return p
	}
	return p + "\n\n---\n\n" + base
}
