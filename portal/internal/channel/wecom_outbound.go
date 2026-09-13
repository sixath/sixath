package channel

import (
	"context"
)

// WeComOutbound 企微群机器人出站实现（单次发送，不重试不落库）。
// 需要重试 + 投递记录的路径继续用 DeliverWeCom（Task 10）。
type WeComOutbound struct {
	webhookURL string
}

// NewWeComOutbound 构造 wecom 出站渠道。
func NewWeComOutbound(webhookURL string) *WeComOutbound {
	return &WeComOutbound{webhookURL: webhookURL}
}

func (w *WeComOutbound) Type() string { return "wecom" }

func (w *WeComOutbound) Send(ctx context.Context, msg OutboundMessage) (DeliveryReceipt, error) {
	if msg.Content == "" {
		return DeliveryReceipt{Status: "skipped"}, nil
	}
	if w.webhookURL == "" {
		return DeliveryReceipt{Status: "failed"}, newConfigError("wecom channel missing webhook_url")
	}
	if err := PushToWeCom(ctx, w.webhookURL, msg.Content, msg.MsgType); err != nil {
		return DeliveryReceipt{Status: "failed", Attempts: 1, LastError: err.Error()}, err
	}
	return DeliveryReceipt{Status: "sent", Attempts: 1}, nil
}