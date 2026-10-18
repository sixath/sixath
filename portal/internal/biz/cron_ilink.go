package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

var cronDeliveryKeys = []string{"delivery_mode", "delivery_channel_id", "delivery_peer_id"}

// SetWeixinIlink enables weixin_ilink delivery validation and session origin lookup.
func (uc *CronUsecase) SetWeixinIlink(channels ChannelRepo, peers ChannelPeerSessionRepo, roster WeixinIlinkRepo) {
	uc.ilinkChannels = channels
	uc.ilinkPeers = peers
	uc.ilinkRoster = roster
}

// IlinkOrigin returns the weixin_ilink channel (channels.id) and raw peer ID that a chat session
// came from; both are empty when the session did not start in a weixin_ilink channel.
func (uc *CronUsecase) IlinkOrigin(ctx context.Context, sessionID string) (channelUUID, peerID string, err error) {
	ch, peer, err := ilinkSessionOrigin(ctx, uc.ilinkPeers, uc.ilinkChannels, sessionID)
	if err != nil || ch == nil {
		return "", "", err
	}
	return ch.ID, peer, nil
}

// validateIlinkUpdate checks the effective delivery only when the update touches it, so pausing a
// task whose peer was later removed from the roster still works.
func (uc *CronUsecase) validateIlinkUpdate(ctx context.Context, id string, updates map[string]any) error {
	if v, ok := updates["delivery_peer_id"].(string); ok {
		v = strings.TrimSpace(v)
		if utf8.RuneCountInString(v) > ilinkPeerIDMaxRunes {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "delivery_peer_id is too long")
		}
		updates["delivery_peer_id"] = v
	}
	if uc.ilinkChannels == nil || uc.ilinkRoster == nil {
		return nil
	}
	touched := false
	for _, k := range cronDeliveryKeys {
		if _, ok := updates[k]; ok {
			touched = true
		}
	}
	if !touched {
		return nil
	}
	cur, err := uc.taskRepo.GetByID(ctx, id)
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrCronTaskNotFound
	}
	if err != nil {
		return err
	}
	mode, channelID, peerID := cur.DeliveryMode, cur.DeliveryChannelID, cur.DeliveryPeerID
	if v, ok := updates["delivery_mode"].(string); ok {
		mode = v
	}
	if v, ok := updates["delivery_channel_id"].(string); ok {
		channelID = v
	}
	if v, ok := updates["delivery_peer_id"].(string); ok {
		peerID = v
	}
	return uc.validateIlinkDelivery(ctx, mode, channelID, peerID)
}

// validateIlinkDelivery requires an allowed roster peer when delivering to a weixin_ilink channel.
// Unknown channels are left to the executor, matching the other channel types.
func (uc *CronUsecase) validateIlinkDelivery(ctx context.Context, mode, channelID, peerID string) error {
	if utf8.RuneCountInString(peerID) > ilinkPeerIDMaxRunes {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "delivery_peer_id is too long")
	}
	if uc.ilinkChannels == nil || uc.ilinkRoster == nil || mode != "channel" || channelID == "" {
		return nil
	}
	ch, err := uc.ilinkChannels.GetByID(ctx, channelID)
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if ch.Type != ChannelTypeWeixinIlink {
		return nil
	}
	if peerID == "" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "投递到微信渠道必须选择接收人（delivery_peer_id）")
	}
	peer, err := uc.ilinkRoster.GetPeer(ctx, ch.ID, peerID)
	if errors.Is(err, pkgErrors.ErrNotFound) || (err == nil && !peer.Allowed) {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", fmt.Sprintf("目标 %s 未在白名单", peerID))
	}
	return err
}
