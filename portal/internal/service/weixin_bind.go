package service

import (
	"context"

	"backend/internal/biz"
	"backend/internal/channel"
	"backend/internal/chat"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/tool"
)

// WeixinIlinkSender is what send_to_weixin needs from biz (*biz.WeixinIlinkUsecase satisfies it).
type WeixinIlinkSender interface {
	chat.WeixinPusher
	ChannelsForAgent(ctx context.Context, agentID string) ([]*biz.ChannelMeta, error)
}

// weixinSenderOrNil maps a typed-nil *biz.WeixinIlinkUsecase to a nil interface.
func weixinSenderOrNil(w WeixinIlinkSender) WeixinIlinkSender {
	if uc, ok := w.(*biz.WeixinIlinkUsecase); ok && uc == nil {
		return nil
	}
	return w
}

// registerWeixinToolForAgent registers send_to_weixin when the agent has at least one enabled
// weixin_ilink channel and returns the matching catalog bindings (nil when not registered).
func registerWeixinToolForAgent(ctx context.Context, logger *log.Helper, sender WeixinIlinkSender, recorder channel.DeliveryRecorder, reg *tool.Registry, agentMeta *biz.AgentMeta) []chat.ChannelBinding {
	if sender == nil || agentMeta == nil {
		return nil
	}
	channels, err := sender.ChannelsForAgent(ctx, agentMeta.ID)
	if err != nil {
		if logger != nil {
			logger.Warnf("send_to_weixin channel lookup failed: agent_id=%s err=%v", agentMeta.ID, err)
		}
		return nil
	}
	if len(channels) == 0 {
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
