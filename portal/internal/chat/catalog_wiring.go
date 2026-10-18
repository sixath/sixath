package chat

import (
	"context"

	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
)

// ChannelBinding 把出站渠道工具绑定到目录；ChannelID 可为逗号分隔的多个渠道 slug。
type ChannelBinding struct {
	ChannelID   string
	ChannelType string
}

// CatalogWiringInput 构建 Agent 工具目录所需的运行时上下文。
type CatalogWiringInput struct {
	Reg            *tool.Registry
	DsBindings     []DatasourceBinding
	WecomChannelID string
	ChannelType    string           // "wecom" when bound
	Channels       []ChannelBinding // 其它出站渠道（如 weixin_ilink），与 WecomChannelID 并存
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
	for _, b := range in.Channels {
		providers = append(providers, &ChannelCatalogProvider{ChannelID: b.ChannelID, ChannelType: b.ChannelType})
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
