package cron

import (
	"context"
	"errors"
	"strings"
	"testing"

	"backend/internal/biz"
	pkgErrors "backend/internal/pkg/errors"

	"github.com/go-kratos/kratos/v2/log"
)

type cronFakeChannels struct {
	biz.ChannelRepo
	byID map[string]*biz.ChannelMeta
}

func (f *cronFakeChannels) GetByID(_ context.Context, id string) (*biz.ChannelMeta, error) {
	ch, ok := f.byID[id]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	return ch, nil
}

type fakeIlinkPusher struct {
	calls []string
	err   error
}

func (f *fakeIlinkPusher) Push(_ context.Context, channelUUID, peerID, text string) ([]string, error) {
	f.calls = append(f.calls, channelUUID+"|"+peerID+"|"+text)
	if f.err != nil {
		return nil, f.err
	}
	return []string{"m1"}, nil
}

func newIlinkDeliveryExecutor() (*Executor, *fakeIlinkPusher) {
	channels := &cronFakeChannels{byID: map[string]*biz.ChannelMeta{
		"uuid-1":  {ID: "uuid-1", ChannelID: "wx-1", Type: biz.ChannelTypeWeixinIlink},
		"uuid-wh": {ID: "uuid-wh", ChannelID: "wh-1", Type: "webhook"},
	}}
	e := &Executor{
		channelUC: biz.NewChannelUsecase(channels, nil, log.DefaultLogger),
		log:       log.NewHelper(log.DefaultLogger),
	}
	p := &fakeIlinkPusher{}
	e.SetIlinkPusher(p)
	return e, p
}

func TestDeliverChannel_WeixinIlink(t *testing.T) {
	e, p := newIlinkDeliveryExecutor()
	ctx := context.Background()
	task := &biz.CronTaskMeta{Name: "日报", DeliveryChannelID: "uuid-1", DeliveryPeerID: "p1@im.wechat"}

	if err := e.deliverChannel(ctx, task, "今天晴", nil); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if len(p.calls) != 1 || p.calls[0] != "uuid-1|p1@im.wechat|[定时任务 日报] 今天晴" {
		t.Fatalf("calls = %q", p.calls)
	}

	p.err = biz.ErrIlinkLoginRequired
	err := e.deliverChannel(ctx, task, "x", nil)
	if !errors.Is(err, biz.ErrIlinkLoginRequired) || !strings.Contains(err.Error(), "微信推送失败") {
		t.Fatalf("push failure err = %v", err)
	}
}

func TestDeliverChannel_WeixinIlinkMisconfigured(t *testing.T) {
	e, p := newIlinkDeliveryExecutor()
	ctx := context.Background()

	err := e.deliverChannel(ctx, &biz.CronTaskMeta{Name: "n", DeliveryChannelID: "uuid-1"}, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "delivery_peer_id") || len(p.calls) != 0 {
		t.Fatalf("missing peer err = %v calls = %d", err, len(p.calls))
	}
	e.SetIlinkPusher(nil)
	if err := e.deliverChannel(ctx, &biz.CronTaskMeta{Name: "n", DeliveryChannelID: "uuid-1", DeliveryPeerID: "p1"}, "x", nil); err == nil {
		t.Fatal("want error when the pusher is not wired")
	}
	var typedNil *biz.WeixinIlinkUsecase
	e.SetIlinkPusher(typedNil)
	if e.ilink != nil {
		t.Fatalf("typed-nil pusher stored as %#v", e.ilink)
	}
	if err := e.deliverChannel(ctx, &biz.CronTaskMeta{Name: "n", DeliveryChannelID: "uuid-1", DeliveryPeerID: "p1"}, "x", nil); err == nil {
		t.Fatal("want error when the pusher is a typed nil")
	}
	err = e.deliverChannel(ctx, &biz.CronTaskMeta{Name: "n", DeliveryChannelID: "uuid-wh"}, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "weixin_ilink") {
		t.Fatalf("unsupported type err = %v", err)
	}
}
