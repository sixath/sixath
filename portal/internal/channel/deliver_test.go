package channel

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

// fakeSend 按脚本依次返回错误（最后一次为 nil 表示成功）。
func fakeSend(script []error) func(context.Context, string, string, string) error {
	var calls int32
	return func(context.Context, string, string, string) error {
		i := atomic.AddInt32(&calls, 1)
		idx := int(i) - 1
		if idx >= len(script) {
			return nil
		}
		return script[idx]
	}
}

func testOpts(maxAttempts int, send func(context.Context, string, string, string) error) DeliverOptions {
	return DeliverOptions{
		MaxAttempts: maxAttempts,
		BaseDelay:   time.Millisecond,
		MaxDelay:    2 * time.Millisecond,
		Send:        send,
	}
}

func TestDeliverWeCom_RetriesOn5xxThenSucceeds(t *testing.T) {
	send := fakeSend([]error{
		&WeComError{HTTPStatus: 500, ErrMsg: "boom"},
		&WeComError{HTTPStatus: 502, ErrMsg: "boom"},
		// 第三次 nil → 成功
	})
	d, err := DeliverWeCom(context.Background(), "https://example", "hello", "text", "ch-1", "sess-1", testOpts(3, send), nil)
	if err != nil {
		t.Fatalf("DeliverWeCom() error = %v", err)
	}
	if d.Status != DeliverySent {
		t.Fatalf("Status = %q, want sent", d.Status)
	}
	if d.Attempts != 3 {
		t.Fatalf("Attempts = %d, want 3", d.Attempts)
	}
	if d.ID == "" {
		t.Fatal("delivery ID should not be empty")
	}
}

func TestDeliverWeCom_MarksFailedAfterMaxAttempts(t *testing.T) {
	send := fakeSend([]error{
		&WeComError{HTTPStatus: 500, ErrMsg: "boom"},
		&WeComError{HTTPStatus: 500, ErrMsg: "boom"},
		&WeComError{HTTPStatus: 500, ErrMsg: "boom"},
	})
	d, err := DeliverWeCom(context.Background(), "https://example", "hello", "text", "ch-1", "sess-1", testOpts(3, send), nil)
	if err == nil {
		t.Fatal("expected final error after max attempts")
	}
	if d.Status != DeliveryFailed {
		t.Fatalf("Status = %q, want failed", d.Status)
	}
	if d.Attempts != 3 {
		t.Fatalf("Attempts = %d, want 3", d.Attempts)
	}
	if !strings.Contains(d.LastError, "HTTP 500") {
		t.Fatalf("LastError = %q, want contains HTTP 500", d.LastError)
	}
}

func TestDeliverWeCom_DoesNotRetryOnInvalidWebhook(t *testing.T) {
	cases := map[string]error{
		"business errcode": &WeComError{ErrCode: 93000, ErrMsg: "invalid webhook url"},
		"http 400":         &WeComError{HTTPStatus: 400, ErrMsg: "bad request"},
		"http 403":         &WeComError{HTTPStatus: 403, ErrMsg: "forbidden"},
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			send := fakeSend([]error{err})
			d, _ := DeliverWeCom(context.Background(), "https://example", "hello", "text", "ch-1", "sess-1", testOpts(5, send), nil)
			if d.Status != DeliveryFailed {
				t.Fatalf("Status = %q, want failed", d.Status)
			}
			if d.Attempts != 1 {
				t.Fatalf("Attempts = %d, want 1 (no retry on non-retryable)", d.Attempts)
			}
		})
	}
}

func TestDeliverWeCom_RecordsResult(t *testing.T) {
	rec := &captureRecorder{}
	d, err := DeliverWeCom(context.Background(), "https://example", "hello", "markdown", "ch-rec", "sess-rec", testOpts(2, fakeSend([]error{
		&WeComError{HTTPStatus: 500},
	})), rec)
	if err != nil {
		t.Fatalf("DeliverWeCom() error = %v", err)
	}
	if rec.record.ID != d.ID {
		t.Fatalf("record.ID = %q, want %q", rec.record.ID, d.ID)
	}
	if rec.record.Status != DeliverySent || rec.record.Attempts != 2 {
		t.Fatalf("record = %+v", rec.record)
	}
	if rec.record.ChannelID != "ch-rec" || rec.record.SessionID != "sess-rec" {
		t.Fatalf("record ids = %q/%q", rec.record.ChannelID, rec.record.SessionID)
	}
	if rec.record.MsgType != "markdown" {
		t.Fatalf("record.MsgType = %q", rec.record.MsgType)
	}
}

func TestDeliverWeCom_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	d, _ := DeliverWeCom(ctx, "https://example", "hello", "text", "ch-1", "sess-1", DeliverOptions{
		MaxAttempts: 5,
		BaseDelay:   5 * time.Second, // 长退避，会被 ctx 取消打断
		MaxDelay:    5 * time.Second,
		Send:        fakeSend([]error{&WeComError{HTTPStatus: 500}}),
	}, nil)
	if d.Status != DeliveryFailed {
		t.Fatalf("Status = %q, want failed on cancel", d.Status)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("took %v, want to stop promptly on ctx cancel", elapsed)
	}
}

type captureRecorder struct {
	record DeliveryRecord
}

func (c *captureRecorder) Record(_ context.Context, rec DeliveryRecord) error {
	c.record = rec
	return nil
}

// scriptedOutbound 按脚本依次返回错误（耗尽后成功），用于测试渠道无关的 Deliver。
type scriptedOutbound struct {
	calls int
	errs  []error
}

func (s *scriptedOutbound) Type() string { return "test" }

func (s *scriptedOutbound) Send(_ context.Context, _ OutboundMessage) (DeliveryReceipt, error) {
	i := s.calls
	s.calls++
	if i < len(s.errs) && s.errs[i] != nil {
		return DeliveryReceipt{Status: "failed", Attempts: 1, LastError: s.errs[i].Error()}, s.errs[i]
	}
	return DeliveryReceipt{Status: "sent", Attempts: 1}, nil
}

func TestDeliver_RetriesOnRetryableThenSucceeds(t *testing.T) {
	out := &scriptedOutbound{errs: []error{
		&WeComError{HTTPStatus: 500, ErrMsg: "boom"},
		&WeComError{HTTPStatus: 502, ErrMsg: "boom"},
	}}
	d, err := Deliver(context.Background(), out, OutboundMessage{Content: "hello"}, "ch-1", "sess-1", DeliverOptions{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond}, nil)
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if d.Status != DeliverySent || d.Attempts != 3 {
		t.Fatalf("d = %+v, want sent attempts=3", d)
	}
}

func TestDeliver_StopsOnNonRetryable(t *testing.T) {
	out := &scriptedOutbound{errs: []error{&WeComError{ErrCode: 93000, ErrMsg: "invalid webhook url"}}}
	d, err := Deliver(context.Background(), out, OutboundMessage{Content: "hello"}, "ch-1", "sess-1", DeliverOptions{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond}, nil)
	if err == nil {
		t.Fatal("want final error")
	}
	if d.Status != DeliveryFailed || d.Attempts != 1 {
		t.Fatalf("d = %+v, want failed attempts=1 (no retry)", d)
	}
}

func TestTruncateUTF8_SafeAtBoundary(t *testing.T) {
	// 中文 3 字节/字；4096 不是 3 的倍数，截断绝不能切坏多字节字符。
	s := strings.Repeat("你", 2000) // 6000 字节 > 4096
	got := truncateUTF8(s, wecomMaxContentBytes)
	if len(got) > wecomMaxContentBytes {
		t.Fatalf("len = %d, want <= %d", len(got), wecomMaxContentBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatal("truncated string is not valid UTF-8")
	}
	// 短字符串应原样保留。
	if truncateUTF8("abc", wecomMaxContentBytes) != "abc" {
		t.Fatal("short string should be unchanged")
	}
}