package chat

import (
	"context"
	"testing"

	"backend/internal/biz"
	pkgErrors "backend/internal/pkg/errors"

	"github.com/sixath/framework/tool"
)

type originChannels struct {
	biz.ChannelRepo
	list []*biz.ChannelMeta
}

func (f *originChannels) GetByID(_ context.Context, id string) (*biz.ChannelMeta, error) {
	for _, ch := range f.list {
		if ch.ID == id {
			return ch, nil
		}
	}
	return nil, pkgErrors.ErrNotFound
}

func (f *originChannels) GetByChannelID(_ context.Context, slug string) (*biz.ChannelMeta, error) {
	for _, ch := range f.list {
		if ch.ChannelID == slug {
			return ch, nil
		}
	}
	return nil, pkgErrors.ErrNotFound
}

type originPeers struct {
	biz.ChannelPeerSessionRepo
	bySession map[string]*biz.ChannelPeerSession
}

func (f *originPeers) GetBySessionID(_ context.Context, sessionID string) (*biz.ChannelPeerSession, error) {
	row, ok := f.bySession[sessionID]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	return row, nil
}

type allowAllRoster struct{ biz.WeixinIlinkRepo }

func (allowAllRoster) GetPeer(_ context.Context, _, peerID string) (*biz.IlinkPeer, error) {
	return &biz.IlinkPeer{PeerID: peerID, Allowed: true}, nil
}

type captureCronCreateRepo struct {
	biz.CronTaskRepo
	last *biz.CronTaskCreate
}

func (r *captureCronCreateRepo) Create(_ context.Context, t *biz.CronTaskCreate) (*biz.CronTaskMeta, error) {
	cp := *t
	r.last = &cp
	return &biz.CronTaskMeta{ID: "t1", Name: t.Name, DeliveryMode: t.DeliveryMode}, nil
}

func newOriginCronClient() (tool.CronClient, *captureCronCreateRepo) {
	repo := &captureCronCreateRepo{}
	uc := biz.NewCronUsecase(repo, stubCronRunRepo{}, nil)
	uc.SetWeixinIlink(
		&originChannels{list: []*biz.ChannelMeta{
			{ID: "uuid-wx", ChannelID: "wx-1", Type: biz.ChannelTypeWeixinIlink, Enabled: true},
			{ID: "uuid-bot", ChannelID: "bot-1", Type: "wecom_bot", Enabled: true},
		}},
		&originPeers{bySession: map[string]*biz.ChannelPeerSession{
			"s-wx":  {ChannelID: "wx-1", PeerID: "user:p1@im.wechat", SessionID: "s-wx"},
			"s-bot": {ChannelID: "bot-1", PeerID: "user:zhangsan", SessionID: "s-bot"},
		}},
		allowAllRoster{},
	)
	return NewPortalCronClient(uc, nil), repo
}

func createIn(mode, channelID, webhookURL string) tool.CronJobCreateInput {
	return tool.CronJobCreateInput{
		Name: "daily", ScheduleKind: "cron", ScheduleExpr: "0 9 * * *", PayloadKind: "agent_turn", PayloadContent: "x",
		DeliveryMode: mode, DeliveryChannelID: channelID, DeliveryWebhookURL: webhookURL, Enabled: true,
	}
}

func TestPortalCronClient_BackfillsWeixinOrigin(t *testing.T) {
	client, repo := newOriginCronClient()
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "s-wx")

	if _, err := client.Create(ctx, "agent-1", createIn("", "", "")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := repo.last
	if got.DeliveryMode != "channel" || got.DeliveryChannelID != "uuid-wx" || got.DeliveryPeerID != "p1@im.wechat" {
		t.Fatalf("delivery = %q / %q / %q", got.DeliveryMode, got.DeliveryChannelID, got.DeliveryPeerID)
	}
}

func TestPortalCronClient_NoBackfill(t *testing.T) {
	client, repo := newOriginCronClient()
	cases := []struct {
		session, mode, channelID, webhook string
		wantMode, wantChannel             string
	}{
		{"", "", "", "", "none", ""},
		{"s-unknown", "", "", "", "none", ""},
		{"s-bot", "", "", "", "none", ""},
		{"s-wx", "none", "", "", "none", ""},
		{"s-wx", "", "", "https://example.com/hook", "none", ""},
		{"s-wx", "channel", "uuid-other", "", "channel", "uuid-other"},
	}
	for _, c := range cases {
		ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, c.session)
		if _, err := client.Create(ctx, "agent-1", createIn(c.mode, c.channelID, c.webhook)); err != nil {
			t.Fatalf("%+v: Create: %v", c, err)
		}
		got := repo.last
		if got.DeliveryMode != c.wantMode || got.DeliveryChannelID != c.wantChannel || got.DeliveryPeerID != "" {
			t.Fatalf("%+v: delivery = %q / %q / %q", c, got.DeliveryMode, got.DeliveryChannelID, got.DeliveryPeerID)
		}
	}
}
