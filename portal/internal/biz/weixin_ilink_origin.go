package biz

import (
	"context"
	"errors"
	"strings"

	pkgErrors "backend/internal/pkg/errors"
)

// ilinkPeerKeyPrefix prefixes raw iLink user IDs in channel_peer_sessions.peer_id (the Gateway peer key).
const ilinkPeerKeyPrefix = "user:"

// ilinkSessionOrigin finds the weixin_ilink channel and raw peer ID a chat session came from;
// it returns (nil, "", nil) when the session did not start in a weixin_ilink channel.
func ilinkSessionOrigin(ctx context.Context, peers ChannelPeerSessionRepo, channels ChannelRepo, sessionID string) (*ChannelMeta, string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if peers == nil || channels == nil || sessionID == "" {
		return nil, "", nil
	}
	row, err := peers.GetBySessionID(ctx, sessionID)
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	ch, err := channels.GetByChannelID(ctx, row.ChannelID)
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	peerID := strings.TrimPrefix(row.PeerID, ilinkPeerKeyPrefix)
	if ch.Type != ChannelTypeWeixinIlink || peerID == "" {
		return nil, "", nil
	}
	return ch, peerID, nil
}
