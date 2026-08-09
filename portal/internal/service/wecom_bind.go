package service

import (
	"context"
	"fmt"

	"backend/internal/biz"
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

func registerWeComToolForAgent(ctx context.Context, channelUC *biz.ChannelUsecase, reg *tool.Registry, agentMeta *biz.AgentMeta) {
	channelID := resolveAgentWecomChannelID(ctx, channelUC, agentMeta)
	if channelID == "" {
		return
	}
	_ = chat.RegisterSendToWeComTool(reg, chat.SendToWeComOptions{
		ResolveWebhook: func(ctx context.Context) (string, error) {
			ch, err := channelUC.Get(ctx, channelID)
			if err != nil {
				return "", err
			}
			if ch.Type != "wecom" || !ch.Enabled {
				return "", fmt.Errorf("wecom channel not available")
			}
			if ch.WebhookURL == "" {
				return "", fmt.Errorf("wecom channel missing webhook_url")
			}
			return ch.WebhookURL, nil
		},
	})
}

func appendWecomBoundSystemPrompt(ctx context.Context, channelUC *biz.ChannelUsecase, prompt string, agentMeta *biz.AgentMeta) string {
	if !agentHasWecomOutbound(ctx, channelUC, agentMeta) {
		return prompt
	}
	return prompt + "\n你已绑定企业微信群，可使用 send_to_wecom 工具将结论推送到群；禁止通过 ask_user 向用户索取 Webhook URL。"
}
