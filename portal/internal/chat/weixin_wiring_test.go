package chat

import (
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/channel"

	"github.com/sixath/framework/tool"
)

type fakeWeixinPusher struct {
	resolveErr error
	pushErr    error
	resolved   []string
	pushed     []string
}

func (f *fakeWeixinPusher) ResolveSendTarget(_ context.Context, agentID, sessionID, channelSlug, peerID string) (*biz.IlinkSendTarget, error) {
	f.resolved = append(f.resolved, strings.Join([]string{agentID, sessionID, channelSlug, peerID}, "|"))
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	if peerID == "" {
		peerID = "o9cq800Ngkx4@im.wechat"
	}
	return &biz.IlinkSendTarget{Channel: &biz.ChannelMeta{ID: "uuid-wx", ChannelID: "wx-1"}, PeerID: peerID}, nil
}

func (f *fakeWeixinPusher) Push(_ context.Context, channelUUID, peerID, text string) ([]string, error) {
	f.pushed = append(f.pushed, channelUUID+"|"+peerID+"|"+text)
	if f.pushErr != nil {
		return nil, f.pushErr
	}
	return []string{"m1"}, nil
}

type captureRecorder struct{ recs []channel.DeliveryRecord }

func (r *captureRecorder) Record(_ context.Context, rec channel.DeliveryRecord) error {
	r.recs = append(r.recs, rec)
	return nil
}

func registerWeixinForTest(t *testing.T, p *fakeWeixinPusher, rec channel.DeliveryRecorder) tool.Tool {
	t.Helper()
	reg := tool.NewRegistry()
	if err := RegisterSendToWeixinTool(reg, SendToWeixinOptions{AgentID: "agent-1", Pusher: p, Recorder: rec, RateLimitInterval: time.Millisecond}); err != nil {
		t.Fatalf("register: %v", err)
	}
	tdef, ok := reg.Get("send_to_weixin")
	if !ok {
		t.Fatal("send_to_weixin not registered")
	}
	return tdef
}

func TestRegisterSendToWeixinTool_NilPusher(t *testing.T) {
	reg := tool.NewRegistry()
	if err := RegisterSendToWeixinTool(reg, SendToWeixinOptions{AgentID: "agent-1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("send_to_weixin"); ok {
		t.Fatal("send_to_weixin registered without a pusher")
	}
}

func TestSendToWeixin_SuccessRecordsDelivery(t *testing.T) {
	p := &fakeWeixinPusher{}
	rec := &captureRecorder{}
	tdef := registerWeixinForTest(t, p, rec)
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-wx-ok")

	out, err := tdef.Execute(ctx, map[string]any{"text": "你好"})
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := out.(string)
	if !strings.Contains(msg, "已推送到微信") || !strings.Contains(msg, "wx-1") || !strings.Contains(msg, "gkx4") || !strings.Contains(msg, "delivery_id") {
		t.Fatalf("result = %q", msg)
	}
	if len(p.resolved) != 1 || p.resolved[0] != "agent-1|sess-wx-ok||" {
		t.Fatalf("resolved = %q", p.resolved)
	}
	if len(p.pushed) != 1 || p.pushed[0] != "uuid-wx|o9cq800Ngkx4@im.wechat|你好" {
		t.Fatalf("pushed = %q", p.pushed)
	}
	if len(rec.recs) != 1 || rec.recs[0].Status != channel.DeliverySent || rec.recs[0].ChannelID != "uuid-wx" || rec.recs[0].SessionID != "sess-wx-ok" {
		t.Fatalf("records = %+v", rec.recs)
	}
}

func TestSendToWeixin_FailureIsNotRetried(t *testing.T) {
	p := &fakeWeixinPusher{pushErr: &net.OpError{Op: "dial", Err: errors.New("connection reset")}}
	rec := &captureRecorder{}
	tdef := registerWeixinForTest(t, p, rec)
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-wx-fail")

	out, _ := tdef.Execute(ctx, map[string]any{"text": "x", "peer_id": "p2", "channel_id": "wx-1"})
	msg, _ := out.(string)
	if !strings.Contains(msg, "投递失败") || !strings.Contains(msg, "connection reset") {
		t.Fatalf("result = %q", msg)
	}
	if len(p.pushed) != 1 {
		t.Fatalf("push attempts = %d, want 1", len(p.pushed))
	}
	if len(rec.recs) != 1 || rec.recs[0].Status != channel.DeliveryFailed || rec.recs[0].Attempts != 1 {
		t.Fatalf("records = %+v", rec.recs)
	}
	if p.resolved[0] != "agent-1|sess-wx-fail|wx-1|p2" {
		t.Fatalf("resolved = %q", p.resolved)
	}
}

func TestSendToWeixin_ResolveErrorAndValidation(t *testing.T) {
	p := &fakeWeixinPusher{resolveErr: errors.New("请指定 peer_id")}
	tdef := registerWeixinForTest(t, p, nil)
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-wx-resolve")

	out, _ := tdef.Execute(ctx, map[string]any{"text": " "})
	if msg, _ := out.(string); !strings.Contains(msg, "text is required") {
		t.Fatalf("blank text result = %q", msg)
	}
	out, _ = tdef.Execute(ctx, map[string]any{"text": "x"})
	if msg, _ := out.(string); !strings.Contains(msg, "请指定 peer_id") || len(p.pushed) != 0 {
		t.Fatalf("resolve error result = %q pushed = %d", msg, len(p.pushed))
	}
}

func TestSendToWeixin_RateLimitedPerSession(t *testing.T) {
	p := &fakeWeixinPusher{}
	reg := tool.NewRegistry()
	if err := RegisterSendToWeixinTool(reg, SendToWeixinOptions{AgentID: "agent-1", Pusher: p, RateLimitInterval: time.Hour}); err != nil {
		t.Fatal(err)
	}
	tdef, _ := reg.Get("send_to_weixin")
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-wx-rate")

	_, _ = tdef.Execute(ctx, map[string]any{"text": "1"})
	out, _ := tdef.Execute(ctx, map[string]any{"text": "2"})
	if msg, _ := out.(string); !strings.Contains(msg, "发送过于频繁") || len(p.pushed) != 1 {
		t.Fatalf("second send = %q pushed = %d", msg, len(p.pushed))
	}
}

func TestSendToWeixin_NoSessionIsNotRateLimited(t *testing.T) {
	p := &fakeWeixinPusher{}
	reg := tool.NewRegistry()
	if err := RegisterSendToWeixinTool(reg, SendToWeixinOptions{AgentID: "agent-1", Pusher: p, RateLimitInterval: time.Hour}); err != nil {
		t.Fatal(err)
	}
	tdef, _ := reg.Get("send_to_weixin")

	for i, text := range []string{"cron-a", "cron-b"} {
		out, _ := tdef.Execute(context.Background(), map[string]any{"text": text, "peer_id": "p1"})
		if msg, _ := out.(string); !strings.Contains(msg, "已推送到微信") {
			t.Fatalf("send %d without session = %q", i, msg)
		}
	}
	if len(p.pushed) != 2 {
		t.Fatalf("pushed = %d, want 2", len(p.pushed))
	}
}

func TestSendToWeixin_ResolveErrorDoesNotConsumeRateLimit(t *testing.T) {
	p := &fakeWeixinPusher{resolveErr: errors.New("peer_id 未在白名单")}
	reg := tool.NewRegistry()
	if err := RegisterSendToWeixinTool(reg, SendToWeixinOptions{AgentID: "agent-1", Pusher: p, RateLimitInterval: time.Hour}); err != nil {
		t.Fatal(err)
	}
	tdef, _ := reg.Get("send_to_weixin")
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-wx-argerr")

	out, _ := tdef.Execute(ctx, map[string]any{"text": "1", "peer_id": "bad"})
	if msg, _ := out.(string); !strings.Contains(msg, "未在白名单") {
		t.Fatalf("resolve error result = %q", msg)
	}
	p.resolveErr = nil
	out, _ = tdef.Execute(ctx, map[string]any{"text": "2", "peer_id": "p1"})
	if msg, _ := out.(string); !strings.Contains(msg, "已推送到微信") || len(p.pushed) != 1 {
		t.Fatalf("send after resolve error = %q pushed = %d", msg, len(p.pushed))
	}
}

func TestCatalogWiring_WecomAndWeixinBindings(t *testing.T) {
	reg := tool.NewRegistry()
	if err := RegisterSendToWeComTool(reg, SendToWeComOptions{ResolveWebhook: func(context.Context) (string, error) { return "http://x", nil }}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterSendToWeixinTool(reg, SendToWeixinOptions{AgentID: "agent-1", Pusher: &fakeWeixinPusher{}}); err != nil {
		t.Fatal(err)
	}

	cat := BuildCatalogForAgent(context.Background(), CatalogWiringInput{
		Reg:            reg,
		WecomChannelID: "ch-wecom",
		ChannelType:    "wecom",
		Channels:       WeixinCatalogBindings([]string{"wx-1", "wx-2"}),
	})
	byName := map[string]tool.ToolCatalogEntry{}
	for _, e := range cat.Entries {
		byName[e.Name] = e
	}
	wecom, wx := byName["send_to_wecom"], byName["send_to_weixin"]
	if wecom.Bindings["channel_id"] != "ch-wecom" || wecom.Bindings["channel_type"] != "wecom" {
		t.Fatalf("send_to_wecom bindings = %v", wecom.Bindings)
	}
	if wx.Bindings["channel_id"] != "wx-1,wx-2" || wx.Bindings["channel_type"] != "weixin_ilink" {
		t.Fatalf("send_to_weixin bindings = %v", wx.Bindings)
	}
	for _, want := range []string{"微信", "weixin", "推送"} {
		if !slices.Contains(wx.SearchHints, want) {
			t.Fatalf("send_to_weixin hints missing %q: %v", want, wx.SearchHints)
		}
	}
	if WeixinCatalogBindings(nil) != nil {
		t.Fatal("no slugs must yield no binding")
	}
}

func TestWeixinPeerTail(t *testing.T) {
	if got := weixinPeerTail("o9cq800VxswKIZHziZ1yez1Ngkx4@im.wechat"); got != "gkx4" {
		t.Fatalf("tail = %q", got)
	}
	if got := weixinPeerTail("ab"); got != "ab" {
		t.Fatalf("short tail = %q", got)
	}
}
