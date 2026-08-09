package chat

import (
	"context"

	"github.com/sixath/framework/tool"
)

var channelTypeTools = map[string][]string{
	"wecom": {"send_to_wecom"},
}

var channelTypeHints = map[string][]string{
	"wecom": {"企微", "企业微信", "推送", "webhook"},
}

// ChannelCatalogProvider 为出站 Channel 工具注入 channel_id / channel_type 绑定与检索词。
type ChannelCatalogProvider struct {
	ChannelID   string
	ChannelType string
}

func (p *ChannelCatalogProvider) Enrich(_ context.Context, entries []tool.ToolCatalogEntry) []tool.ToolCatalogEntry {
	if p.ChannelID == "" || p.ChannelType == "" {
		return entries
	}
	toolNames, ok := channelTypeTools[p.ChannelType]
	if !ok {
		return entries
	}
	targets := make(map[string]struct{}, len(toolNames))
	for _, name := range toolNames {
		targets[name] = struct{}{}
	}
	hints := channelTypeHints[p.ChannelType]
	bindings := map[string]string{
		"channel_id":   p.ChannelID,
		"channel_type": p.ChannelType,
	}

	out := make([]tool.ToolCatalogEntry, len(entries))
	for i, e := range entries {
		out[i] = e
		if _, ok := targets[e.Name]; !ok {
			continue
		}
		out[i].Bindings = bindings
		out[i].SearchHints = mergeCatalogHints(e.SearchHints, hints...)
	}
	return out
}
