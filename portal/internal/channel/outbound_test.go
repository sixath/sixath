package channel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewOutbound_Types(t *testing.T) {
	wecom, err := NewOutbound(OutboundConfig{Type: "wecom", WebhookURL: "https://x"})
	if err != nil {
		t.Fatalf("NewOutbound wecom: %v", err)
	}
	if wecom.Type() != "wecom" {
		t.Fatalf("wecom.Type() = %q", wecom.Type())
	}
	if _, ok := wecom.(*WeComOutbound); !ok {
		t.Fatalf("expected *WeComOutbound, got %T", wecom)
	}

	wx, err := NewOutbound(OutboundConfig{Type: "wxpusher", AppToken: "t", DefaultUids: []string{"u1"}})
	if err != nil {
		t.Fatalf("NewOutbound wxpusher: %v", err)
	}
	if _, ok := wx.(*WxPusherOutbound); !ok {
		t.Fatalf("expected *WxPusherOutbound, got %T", wx)
	}
}

func TestNewOutbound_UnsupportedType(t *testing.T) {
	if _, err := NewOutbound(OutboundConfig{Type: "email"}); err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestWeComOutbound_SendSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	ob := NewWeComOutbound(srv.URL)
	receipt, err := ob.Send(context.Background(), OutboundMessage{Content: "hello", MsgType: "text"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if receipt.Status != "sent" {
		t.Fatalf("Status = %q, want sent", receipt.Status)
	}
}

func TestWeComOutbound_SendErrCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	}))
	defer srv.Close()

	ob := NewWeComOutbound(srv.URL)
	_, err := ob.Send(context.Background(), OutboundMessage{Content: "hello"})
	if err == nil {
		t.Fatal("expected errcode error")
	}
}

func TestWeComOutbound_MissingWebhookURL(t *testing.T) {
	ob := NewWeComOutbound("")
	_, err := ob.Send(context.Background(), OutboundMessage{Content: "hello"})
	var cfg *ConfigError
	if !errors.As(err, &cfg) {
		t.Fatalf("expected ConfigError, got %v", err)
	}
}

func TestWeComOutbound_EmptyContentSkips(t *testing.T) {
	ob := NewWeComOutbound("https://x")
	receipt, err := ob.Send(context.Background(), OutboundMessage{})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if receipt.Status != "skipped" {
		t.Fatalf("Status = %q, want skipped", receipt.Status)
	}
}

func TestWxPusherOutbound_MissingConfig(t *testing.T) {
	// 缺 app_token
	if _, err := NewWxPusherOutbound("", []string{"u1"}).Send(context.Background(), OutboundMessage{Content: "x"}); err == nil {
		t.Fatal("expected error for missing app_token")
	}
	// 无接收者
	_, err := NewWxPusherOutbound("t", nil).Send(context.Background(), OutboundMessage{Content: "x"})
	var cfg *ConfigError
	if !errors.As(err, &cfg) {
		t.Fatalf("expected ConfigError for missing recipients, got %v", err)
	}
}

func TestWxPusherOutbound_EmptyContentSkips(t *testing.T) {
	ob := NewWxPusherOutbound("t", []string{"u1"})
	receipt, err := ob.Send(context.Background(), OutboundMessage{})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if receipt.Status != "skipped" {
		t.Fatalf("Status = %q, want skipped", receipt.Status)
	}
}

func TestOutboundRegistry_CustomPlugin(t *testing.T) {
	r := NewOutboundRegistry()
	r.Register("slack", func(OutboundConfig) (OutboundChannel, error) {
		return NewWeComOutbound("https://slack-hook"), nil // 仅为类型断言
	})
	ob, err := r.New(OutboundConfig{Type: "slack"})
	if err != nil {
		t.Fatalf("New slack: %v", err)
	}
	if ob.Type() != "wecom" { // 插件返回的是 WeComOutbound，Type 仍为 wecom
		t.Fatalf("Type = %q", ob.Type())
	}
}
