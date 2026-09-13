package channel

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/google/uuid"
)

// DeliveryStatus 一次出站投递的终态。
type DeliveryStatus string

const (
	DeliveryPending DeliveryStatus = "pending"
	DeliverySent    DeliveryStatus = "sent"
	DeliveryFailed  DeliveryStatus = "failed"
)

// Delivery 一次出站投递的结果（含重试后终态）。
type Delivery struct {
	ID        string
	Status    DeliveryStatus
	Attempts  int
	LastError string
}

// DeliveryRecord 持久化投递记录所需的字段。NextRetryAt 为异步 outbox 预留：
// 当前为同步投递（立即得终态），该字段恒为 nil；未来接后台 worker 时用于
// 「pending + 下次重试时间」的调度。
type DeliveryRecord struct {
	ID          string
	ChannelID   string
	SessionID   string
	Content     string
	MsgType     string
	Status      DeliveryStatus
	Attempts    int
	LastError   string
	NextRetryAt *time.Time
}

// DeliveryRecorder 投递记录持久化接口。nil 表示不落库（仅发送）。
type DeliveryRecorder interface {
	Record(ctx context.Context, rec DeliveryRecord) error
}

// DeliverOptions 控制投递重试与退避。
type DeliverOptions struct {
	MaxAttempts int                                                           // 总尝试次数，<=0 默认 5
	BaseDelay   time.Duration                                                 // 初始退避，<=0 默认 500ms
	MaxDelay    time.Duration                                                 // 退避上限，<=0 默认 10m
	Send        func(ctx context.Context, url, content, msgType string) error // 仅 DeliverWeCom 使用（测试注入）
}

// Deliver 以指数退避重试向任意 OutboundChannel 投递，并（可选）记录投递结果。
//
// 重试语义：仅可重试错误（WeComError.Retryable 或传输层 net.Error）重试；
// 配置缺失 / 4xx / 业务 errcode 立即失败。
// 返回最终的 Delivery（Status=sent 或 failed）与最终错误（成功为 nil）。
// recorder 为 nil 时跳过落库。
func Deliver(ctx context.Context, out OutboundChannel, msg OutboundMessage, channelID, sessionID string, opts DeliverOptions, recorder DeliveryRecorder) (Delivery, error) {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 5
	}
	if opts.BaseDelay <= 0 {
		opts.BaseDelay = 500 * time.Millisecond
	}
	if opts.MaxDelay <= 0 {
		opts.MaxDelay = 10 * time.Minute
	}

	d := Delivery{ID: uuid.NewString()}
	delay := opts.BaseDelay
	var lastErr error
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		d.Attempts = attempt
		if _, err := out.Send(ctx, msg); err == nil {
			d.Status = DeliverySent
			lastErr = nil
			break
		} else {
			lastErr = err
			if !retryableError(err) {
				d.Status = DeliveryFailed
				break
			}
			if attempt == opts.MaxAttempts {
				d.Status = DeliveryFailed
				break
			}
		}
		// 还剩重试次数：退避等待，或随 ctx 取消而中止。
		if !sleepCtx(ctx, delay) {
			d.Status = DeliveryFailed
			break
		}
		delay *= 2
		if delay > opts.MaxDelay {
			delay = opts.MaxDelay
		}
	}
	if d.Status == "" {
		d.Status = DeliveryFailed
	}
	if lastErr != nil {
		d.LastError = lastErr.Error()
	}

	if recorder != nil {
		rec := DeliveryRecord{
			ID:        d.ID,
			ChannelID: channelID,
			SessionID: sessionID,
			Content:   msg.Content,
			MsgType:   msg.MsgType,
			Status:    d.Status,
			Attempts:  d.Attempts,
			LastError: d.LastError,
		}
		if err := recorder.Record(ctx, rec); err != nil {
			return d, err
		}
	}
	return d, lastErr
}

// DeliverWeCom 保持 wecom 单发适配语义（Send 注入 + webhookURL），复用 Deliver 的重试 + 记录核心。
func DeliverWeCom(ctx context.Context, webhookURL, content, msgType, channelID, sessionID string, opts DeliverOptions, recorder DeliveryRecorder) (Delivery, error) {
	send := opts.Send
	if send == nil {
		send = PushToWeCom
	}
	out := &sendFuncOutbound{send: send, webhookURL: webhookURL}
	return Deliver(ctx, out, OutboundMessage{Content: content, MsgType: msgType}, channelID, sessionID, opts, recorder)
}

// sendFuncOutbound 把 wecom 的 Send(url, content, msgType) 适配为 OutboundChannel。
type sendFuncOutbound struct {
	send       func(ctx context.Context, url, content, msgType string) error
	webhookURL string
}

func (s *sendFuncOutbound) Type() string { return "wecom" }

func (s *sendFuncOutbound) Send(ctx context.Context, msg OutboundMessage) (DeliveryReceipt, error) {
	if msg.Content == "" {
		return DeliveryReceipt{Status: "skipped"}, nil
	}
	if err := s.send(ctx, s.webhookURL, msg.Content, msg.MsgType); err != nil {
		return DeliveryReceipt{Status: "failed", Attempts: 1, LastError: err.Error()}, err
	}
	return DeliveryReceipt{Status: "sent", Attempts: 1}, nil
}

// retryableError 判定错误是否值得重试。
func retryableError(err error) bool {
	if err == nil {
		return false
	}
	var we *WeComError
	if errors.As(err, &we) {
		return we.Retryable()
	}
	// 非 WeComError：仅传输层（net.Error）视为可重试，其余保守不重试。
	var ne net.Error
	return errors.As(err, &ne)
}

// sleepCtx 等待 d，ctx 取消时返回 false。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}