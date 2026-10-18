package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"backend/internal/ilink"
	pkgErrors "backend/internal/pkg/errors"
)

// IlinkSender is the outbound subset of the iLink client (*ilink.Client satisfies it).
type IlinkSender interface {
	SendText(ctx context.Context, to, contextToken, text string) (string, error)
}

// Push errors are plain errors: their text is stored as cron delivery_error and shown to the agent.
var (
	ErrIlinkLoginRequired  = errors.New("微信 bot 需重新扫码登录")
	ErrIlinkPeerNotAllowed = errors.New("未在白名单")
	ErrIlinkNoContextToken = errors.New("对方尚未与 bot 对话（或 bot 已重新登录），请对方先给 bot 发一条消息")
)

// Push sends text to an allowed roster peer of a weixin_ilink channel (channelUUID is channels.id).
// Markdown is flattened and split into ilink.DefaultSplitRunes parts. There is no retry: a failed
// sendmessage may still have been delivered. It returns the message ids of the parts sent before any error.
func (uc *WeixinIlinkUsecase) Push(ctx context.Context, channelUUID, peerID, text string) ([]string, error) {
	peerID = strings.TrimSpace(peerID)
	if err := ilinkValidPeerID(peerID); err != nil {
		return nil, err
	}
	ch, err := uc.channelByID(ctx, channelUUID)
	if err != nil {
		return nil, err
	}
	if !ch.Enabled {
		return nil, fmt.Errorf("微信渠道 %s 已停用", ch.ChannelID)
	}
	if ch.IlinkBotToken == "" || ch.IlinkSessionExpiredAt != nil {
		return nil, ErrIlinkLoginRequired
	}
	peer, err := uc.repo.GetPeer(ctx, ch.ID, peerID)
	if errors.Is(err, pkgErrors.ErrNotFound) || (err == nil && !peer.Allowed) {
		return nil, fmt.Errorf("目标 %s %w", peerID, ErrIlinkPeerNotAllowed)
	}
	if err != nil {
		return nil, err
	}
	if peer.ContextToken == "" {
		return nil, ErrIlinkNoContextToken
	}
	parts := ilink.SplitText(ilink.PlainText(text), ilink.DefaultSplitRunes)
	if len(parts) == 0 {
		return nil, errors.New("推送内容为空")
	}
	sender := uc.newSender(ch.IlinkBaseURL, ch.IlinkBotToken)
	ids := make([]string, 0, len(parts))
	for i, part := range parts {
		id, err := sender.SendText(ctx, peerID, peer.ContextToken, part)
		if err != nil {
			if ilink.IsSessionExpired(err) {
				if _, markErr := uc.repo.MarkSessionExpired(ctx, ch.ID, ch.IlinkGeneration, uc.now()); markErr != nil {
					return ids, fmt.Errorf("%w（会话已过期，标记失效失败：%v）", ErrIlinkLoginRequired, markErr)
				}
				return ids, fmt.Errorf("%w（会话已过期）", ErrIlinkLoginRequired)
			}
			return ids, fmt.Errorf("发送第 %d/%d 段失败：%w", i+1, len(parts), err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}
