package service

import (
	"context"
	"fmt"

	"backend/internal/biz"
	"backend/internal/channel"
	"backend/internal/chat"

	"github.com/sixath/framework/tool"
)

// resolveAgentWecomChannelID 优先 Agent.wecom_channel_id，否则回退到 wecom Channel 的 default_agent 绑定。
func resolveAgentWecomChannelID(ctx context.Context, channelUC *biz.ChannelUsecase, agentMeta *biz.AgentMeta) string {
	if agentMeta == nil {
		return ""
	}
	if agentMeta.WecomChannelID != "" {
		return agentMeta.WecomChannelID
	}
	ch, err := channelUC.GetWecomByDefaultAgent(ctx, agentMeta.ID)
	if err != nil || ch == nil {
		return ""
	}
	return ch.ID
}

func agentHasWecomOutbound(ctx context.Context, channelUC *biz.ChannelUsecase, agentMeta *biz.AgentMeta) bool {
	return resolveAgentWecomChannelID(ctx, channelUC, agentMeta) != ""
}

// resolveAgentOutboundChannel 返回 Agent 绑定的出站渠道：显式 wecom 绑定优先，
// 否则回退到 default_agent 绑定的任意出站渠道（wecom / wxpusher）。
func resolveAgentOutboundChannel(ctx context.Context, channelUC *biz.ChannelUsecase, agentMeta *biz.AgentMeta) *biz.ChannelMeta {
	if agentMeta == nil {
		return nil
	}
	if agentMeta.WecomChannelID != "" {
		if ch, err := channelUC.Get(ctx, agentMeta.WecomChannelID); err == nil && ch != nil && ch.Enabled {
			return ch
		}
	}
	ch, err := channelUC.GetOutboundByDefaultAgent(ctx, agentMeta.ID)
	if err != nil || ch == nil {
		return nil
	}
	return ch
}

func registerWeComToolForAgent(ctx context.Context, channelUC *biz.ChannelUsecase, recorder channel.DeliveryRecorder, reg *tool.Registry, agentMeta *biz.AgentMeta) {
	ch := resolveAgentOutboundChannel(ctx, channelUC, agentMeta)
	if ch == nil {
		return
	}
	channelID := ch.ID
	_ = chat.RegisterSendToWeComTool(reg, chat.SendToWeComOptions{
		ChannelID: channelID,
		Recorder:  recorder,
		ResolveOutbound: func(ctx context.Context) (channel.OutboundChannel, error) {
			// 每次调用重新 Get，避免持有过期配置。
			meta, err := channelUC.Get(ctx, channelID)
			if err != nil {
				return nil, err
			}
			if !meta.Enabled {
				return nil, fmt.Errorf("channel not available")
			}
			return channel.NewOutbound(channel.OutboundConfig{
				Type:        meta.Type,
				WebhookURL:  meta.WebhookURL,
				AppToken:    meta.AppToken,
				DefaultUids: meta.DefaultUids,
			})
		},
	})
}

func appendWecomBoundSystemPrompt(ctx context.Context, channelUC *biz.ChannelUsecase, prompt string, agentMeta *biz.AgentMeta) string {
	if !agentHasWecomOutbound(ctx, channelUC, agentMeta) {
		return prompt
	}
	return prompt + "\n你已绑定企业微信群，可使用 send_to_wecom 工具将结论推送到群；禁止通过 ask_user 向用户索取 Webhook URL。"
}
