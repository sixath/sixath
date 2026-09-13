package channel

import (
	"context"
)

// WxPusherOutbound WxPusher 出站实现。
type WxPusherOutbound struct {
	appToken    string
	defaultUids []string
}

// NewWxPusherOutbound 构造 wxpusher 出站渠道。
func NewWxPusherOutbound(appToken string, defaultUids []string) *WxPusherOutbound {
	return &WxPusherOutbound{appToken: appToken, defaultUids: defaultUids}
}

func (w *WxPusherOutbound) Type() string { return "wxpusher" }

func (w *WxPusherOutbound) Send(ctx context.Context, msg OutboundMessage) (DeliveryReceipt, error) {
	if msg.Content == "" {
		return DeliveryReceipt{Status: "skipped"}, nil
	}
	if w.appToken == "" {
		return DeliveryReceipt{Status: "failed"}, newConfigError("wxpusher channel missing app_token")
	}
	uids := msg.Recipients
	if len(uids) == 0 {
		uids = w.defaultUids
	}
	if len(uids) == 0 {
		return DeliveryReceipt{Status: "failed"}, newConfigError("wxpusher channel has no recipients")
	}
	if err := PushToWxPusher(ctx, w.appToken, uids, msg.Content, msg.Summary); err != nil {
		return DeliveryReceipt{Status: "failed", Attempts: 1, LastError: err.Error()}, err
	}
	return DeliveryReceipt{Status: "sent", Attempts: 1}, nil
}
