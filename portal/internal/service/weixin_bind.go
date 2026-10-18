package service

import (
	"context"

	"backend/internal/biz"
	"backend/internal/channel"
	"backend/internal/chat"

	"github.com/sixath/framework/tool"
)

// WeixinIlinkSender is what send_to_weixin needs from biz (*biz.WeixinIlinkUsecase satisfies it).
type WeixinIlinkSender interface {
	chat.WeixinPusher
	ChannelsForAgent(ctx context.Context, agentID string) ([]*biz.ChannelMeta, error)
}

// registerWeixinToolForAgent registers send_to_weixin when the agent has at least one enabled
// weixin_ilink channel and returns the matching catalog bindings (nil when not registered).
func registerWeixinToolForAgent(ctx context.Context, sender WeixinIlinkSender, recorder channel.DeliveryRecorder, reg *tool.Registry, agentMeta *biz.AgentMeta) []chat.ChannelBinding {
	if sender == nil || agentMeta == nil {
		return nil
	}
	channels, err := sender.ChannelsForAgent(ctx, agentMeta.ID)
	if err != nil || len(channels) == 0 {
		return nil
	}
	if err := chat.RegisterSendToWeixinTool(reg, chat.SendToWeixinOptions{AgentID: agentMeta.ID, Pusher: sender, Recorder: recorder}); err != nil {
		return nil
	}
	slugs := make([]string, 0, len(channels))
	for _, ch := range channels {
		slugs = append(slugs, ch.ChannelID)
	}
	return chat.WeixinCatalogBindings(slugs)
}
