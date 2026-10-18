package chat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"backend/internal/biz"
	"backend/internal/channel"

	"github.com/sixath/framework/tool"
)

// sendToWeixinLastSend 按 session_id 记录 send_to_weixin 上次发送时间，进程内限流（与 send_to_wecom 分开计数）。
var sendToWeixinLastSend sync.Map

// WeixinPusher resolves send_to_weixin targets and sends (*biz.WeixinIlinkUsecase satisfies it).
type WeixinPusher interface {
	ResolveSendTarget(ctx context.Context, agentID, sessionID, channelSlug, peerID string) (*biz.IlinkSendTarget, error)
	Push(ctx context.Context, channelUUID, peerID, text string) ([]string, error)
}

// SendToWeixinOptions 构造 send_to_weixin 工具的依赖。
type SendToWeixinOptions struct {
	AgentID string
	Pusher  WeixinPusher
	// Recorder 可选：非空时每次投递（成功或失败）都会落一条 channel_deliveries 记录。
	Recorder channel.DeliveryRecorder
	// RateLimitInterval 同一会话发送的最小间隔，<=0 默认 1s；无会话（如 Cron）时不限流。
	RateLimitInterval time.Duration
}

// RegisterSendToWeixinTool registers send_to_weixin; it is a no-op when reg or Pusher is nil.
func RegisterSendToWeixinTool(reg *tool.Registry, opts SendToWeixinOptions) error {
	if reg == nil || opts.Pusher == nil {
		return nil
	}
	return reg.Register(tool.Tool{
		Name: "send_to_weixin",
		Description: "Send a text message to a WeChat user through the agent's WeChat (weixin_ilink) bot. " +
			"In a WeChat conversation peer_id defaults to the current user; otherwise pass peer_id, and channel_id when the agent has several WeChat channels. " +
			"The recipient must be allowed in the channel roster and must have messaged the bot since its last login. " +
			"通过 Agent 关联的微信 bot 给名册内已放行的微信用户推送文字消息；在微信会话中默认发给当前对话人。",
		Toolset: tool.ToolsetCore,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "Message body (Markdown is flattened to plain text).",
				},
				"peer_id": map[string]any{
					"type":        "string",
					"description": "Recipient WeChat user id (e.g. xxx@im.wechat). Defaults to the current WeChat user.",
				},
				"channel_id": map[string]any{
					"type":        "string",
					"description": "weixin_ilink channel slug; only needed when the agent has several WeChat channels.",
				},
			},
			"required": []string{"text"},
		},
		Execute: buildSendToWeixinExecute(opts),
	})
}

func buildSendToWeixinExecute(opts SendToWeixinOptions) tool.ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		text, _ := params["text"].(string)
		if strings.TrimSpace(text) == "" {
			return "send_to_weixin: text is required", nil
		}
		peerID, _ := params["peer_id"].(string)
		channelSlug, _ := params["channel_id"].(string)
		agentID := opts.AgentID
		if agentID == "" {
			agentID, _ = ctx.Value(tool.ContextKeyAgentID).(string)
		}
		sessionID, _ := ctx.Value(tool.ContextKeySessionID).(string)
		// Session-less runs (cron agent_turn) are not limited, so concurrent tasks don't throttle each other.
		limited := sessionID != ""
		interval := opts.RateLimitInterval
		if interval <= 0 {
			interval = sendToWeComDefaultInterval
		}
		if limited {
			if v, ok := sendToWeixinLastSend.Load(sessionID); ok {
				if last, ok := v.(time.Time); ok && time.Since(last) < interval {
					return fmt.Sprintf("发送过于频繁，请至少间隔 %s 后再试", interval), nil
				}
			}
		}
		target, err := opts.Pusher.ResolveSendTarget(ctx, agentID, sessionID, channelSlug, peerID)
		if err != nil {
			return fmt.Sprintf("send_to_weixin: %v", err), nil
		}
		if limited {
			sendToWeixinLastSend.Store(sessionID, time.Now())
		}
		out := &weixinOutbound{pusher: opts.Pusher, channelUUID: target.Channel.ID, peerID: target.PeerID}
		// MaxAttempts=1: a failed sendmessage may still have been delivered, so never resend.
		d, err := channel.Deliver(ctx, out, channel.OutboundMessage{Content: text, MsgType: "text"}, target.Channel.ID, sessionID, channel.DeliverOptions{MaxAttempts: 1}, opts.Recorder)
		if err != nil {
			return fmt.Sprintf("send_to_weixin: 投递失败（delivery_id: %s）: %v", d.ID, err), nil
		}
		return fmt.Sprintf("已推送到微信（渠道 %s，接收人尾号 %s，delivery_id: %s）", target.Channel.ChannelID, weixinPeerTail(target.PeerID), d.ID), nil
	}
}

// weixinOutbound adapts WeixinPusher to channel.OutboundChannel so deliveries are recorded.
type weixinOutbound struct {
	pusher      WeixinPusher
	channelUUID string
	peerID      string
}

func (w *weixinOutbound) Type() string { return biz.ChannelTypeWeixinIlink }

func (w *weixinOutbound) Send(ctx context.Context, msg channel.OutboundMessage) (channel.DeliveryReceipt, error) {
	if _, err := w.pusher.Push(ctx, w.channelUUID, w.peerID, msg.Content); err != nil {
		return channel.DeliveryReceipt{Status: "failed", Attempts: 1, LastError: err.Error()}, err
	}
	return channel.DeliveryReceipt{Status: "sent", Attempts: 1}, nil
}

// weixinPeerTail returns the last 4 chars of the part before '@' (iLink IDs are opaque).
func weixinPeerTail(peerID string) string {
	local, _, _ := strings.Cut(peerID, "@")
	r := []rune(local)
	if len(r) <= 4 {
		return local
	}
	return string(r[len(r)-4:])
}

// WeixinCatalogBindings binds send_to_weixin in the tool catalog to the agent's weixin_ilink slugs.
func WeixinCatalogBindings(slugs []string) []ChannelBinding {
	if len(slugs) == 0 {
		return nil
	}
	return []ChannelBinding{{ChannelID: strings.Join(slugs, ","), ChannelType: biz.ChannelTypeWeixinIlink}}
}
