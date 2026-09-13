package chat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/internal/channel"

	"github.com/sixath/framework/tool"
)

func TestRegisterSendToWeComTool_NilResolver(t *testing.T) {
	reg := tool.NewRegistry()
	if err := RegisterSendToWeComTool(reg, SendToWeComOptions{}); err != nil {
		t.Fatalf("RegisterSendToWeComTool() error = %v", err)
	}
	if _, ok := reg.Get("send_to_wecom"); ok {
		t.Fatal("send_to_wecom should not be registered when ResolveWebhook is nil")
	}
	for _, tdef := range reg.List() {
		if tdef.Name == "send_to_wecom" {
			t.Fatal("send_to_wecom found in List() with nil resolver")
		}
	}
}

func TestRegisterSendToWeComTool_ExecuteSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	reg := tool.NewRegistry()
	if err := RegisterSendToWeComTool(reg, SendToWeComOptions{
		ResolveWebhook: func(context.Context) (string, error) {
			return srv.URL, nil
		},
	}); err != nil {
		t.Fatalf("RegisterSendToWeComTool() error = %v", err)
	}
	tdef, ok := reg.Get("send_to_wecom")
	if !ok {
		t.Fatal("send_to_wecom not registered")
	}

	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-wecom-ok")
	out, err := tdef.Execute(ctx, map[string]any{"content": "hello team"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	msg, ok := out.(string)
	if !ok {
		t.Fatalf("Execute() result type = %T, want string", out)
	}
	if !strings.Contains(msg, "已投递到企业微信群") || !strings.Contains(msg, "delivery_id") {
		t.Fatalf("Execute() result = %q, want delivery confirmation with id", msg)
	}
}

func TestRegisterSendToWeComTool_ErrCodePropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	}))
	t.Cleanup(srv.Close)

	reg := tool.NewRegistry()
	if err := RegisterSendToWeComTool(reg, SendToWeComOptions{
		ResolveWebhook: func(context.Context) (string, error) {
			return srv.URL, nil
		},
	}); err != nil {
		t.Fatalf("RegisterSendToWeComTool() error = %v", err)
	}
	tdef, ok := reg.Get("send_to_wecom")
	if !ok {
		t.Fatal("send_to_wecom not registered")
	}

	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-wecom-err")
	out, err := tdef.Execute(ctx, map[string]any{"content": "fail case"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	msg, ok := out.(string)
	if !ok {
		t.Fatalf("Execute() result type = %T, want string", out)
	}
	if !strings.Contains(msg, "errcode=93000") {
		t.Fatalf("Execute() result = %q, want errcode in message", msg)
	}
}

func TestRegisterSendToWeComTool_ResolveWebhookError(t *testing.T) {
	reg := tool.NewRegistry()
	if err := RegisterSendToWeComTool(reg, SendToWeComOptions{
		ResolveWebhook: func(context.Context) (string, error) {
			return "", errors.New("no webhook bound")
		},
	}); err != nil {
		t.Fatalf("RegisterSendToWeComTool() error = %v", err)
	}
	tdef, _ := reg.Get("send_to_wecom")
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-resolve-err")
	out, err := tdef.Execute(ctx, map[string]any{"content": "x"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	msg, _ := out.(string)
	if !strings.Contains(msg, "no webhook bound") {
		t.Fatalf("Execute() result = %q, want resolve error", msg)
	}
}

func TestRegisterSendToWeComTool_ResolveOutbound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	reg := tool.NewRegistry()
	if err := RegisterSendToWeComTool(reg, SendToWeComOptions{
		ResolveOutbound: func(context.Context) (channel.OutboundChannel, error) {
			return channel.NewWeComOutbound(srv.URL), nil
		},
	}); err != nil {
		t.Fatalf("RegisterSendToWeComTool() error = %v", err)
	}
	tdef, ok := reg.Get("send_to_wecom")
	if !ok {
		t.Fatal("send_to_wecom not registered")
	}

	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-outbound")
	out, err := tdef.Execute(ctx, map[string]any{"content": "hello team"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	msg := out.(string)
	if !strings.Contains(msg, "已投递到企业微信群") || !strings.Contains(msg, "delivery_id") {
		t.Fatalf("Execute() result = %q, want delivery confirmation", msg)
	}
}

func TestChannelSuccessLabel(t *testing.T) {
	if channelSuccessLabel("wecom") != "已投递到企业微信群" {
		t.Fatalf("wecom label = %q", channelSuccessLabel("wecom"))
	}
	if channelSuccessLabel("wxpusher") != "已投递到 WxPusher" {
		t.Fatalf("wxpusher label = %q", channelSuccessLabel("wxpusher"))
	}
	if channelSuccessLabel("slack") != "已投递" {
		t.Fatalf("other label = %q", channelSuccessLabel("slack"))
	}
}
