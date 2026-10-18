package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// IlinkSendTarget is where send_to_weixin delivers.
type IlinkSendTarget struct {
	Channel *ChannelMeta
	PeerID  string
}

// SetPeerSessions enables looking up the weixin_ilink channel a chat session came from.
func (uc *WeixinIlinkUsecase) SetPeerSessions(peers ChannelPeerSessionRepo) {
	uc.peers = peers
}

// ChannelsForAgent lists enabled weixin_ilink channels whose default or allowed agents include agentID.
func (uc *WeixinIlinkUsecase) ChannelsForAgent(ctx context.Context, agentID string) ([]*ChannelMeta, error) {
	return uc.channels.ListIlinkByAgent(ctx, agentID)
}

// ResolveSendTarget picks the channel and peer for send_to_weixin. Without channelSlug it uses the
// session's weixin_ilink channel, else the agent's only channel; an explicit channelSlug must name the
// session's channel or one of the agent's channels. peerID defaults to the session peer, and only on
// the session's own channel.
func (uc *WeixinIlinkUsecase) ResolveSendTarget(ctx context.Context, agentID, sessionID, channelSlug, peerID string) (*IlinkSendTarget, error) {
	channelSlug = strings.TrimSpace(channelSlug)
	peerID = strings.TrimSpace(peerID)
	origin, originPeer, err := ilinkSessionOrigin(ctx, uc.peers, uc.channels, sessionID)
	if err != nil {
		return nil, err
	}
	ch := origin
	if origin == nil || (channelSlug != "" && channelSlug != origin.ChannelID) {
		list, err := uc.ChannelsForAgent(ctx, agentID)
		if err != nil {
			return nil, err
		}
		ch = nil
		for _, c := range list {
			if channelSlug != "" && c.ChannelID == channelSlug {
				ch = c
			}
		}
		if ch == nil && channelSlug == "" && len(list) == 1 {
			ch = list[0]
		}
		if ch == nil {
			return nil, ilinkChannelChoiceErr(channelSlug, list)
		}
	}
	if peerID == "" {
		if origin == nil || ch.ID != origin.ID {
			return nil, errors.New("请指定 peer_id：当前会话不是来自该微信渠道")
		}
		peerID = originPeer
	}
	return &IlinkSendTarget{Channel: ch, PeerID: peerID}, nil
}

func ilinkChannelChoiceErr(channelSlug string, list []*ChannelMeta) error {
	slugs := make([]string, 0, len(list))
	for _, c := range list {
		slugs = append(slugs, c.ChannelID)
	}
	switch {
	case len(slugs) == 0:
		return errors.New("当前 Agent 未关联启用的微信渠道")
	case channelSlug != "":
		return fmt.Errorf("微信渠道 %s 不可用，可选渠道：%s", channelSlug, strings.Join(slugs, "、"))
	default:
		return fmt.Errorf("当前 Agent 关联了多个微信渠道，请用 channel_id 指定其一：%s", strings.Join(slugs, "、"))
	}
}
