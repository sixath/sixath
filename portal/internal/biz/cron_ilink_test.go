package biz

import (
	"context"
	"strings"
	"testing"
	"time"

	pkgErrors "backend/internal/pkg/errors"

	"github.com/go-kratos/kratos/v2/log"
)

type cronIlinkTaskRepo struct {
	CronTaskRepo
	tasks   map[string]*CronTaskMeta
	created []*CronTaskCreate
	updated []map[string]any
}

func (r *cronIlinkTaskRepo) Create(_ context.Context, t *CronTaskCreate) (*CronTaskMeta, error) {
	cp := *t
	r.created = append(r.created, &cp)
	return &CronTaskMeta{ID: "t-new", DeliveryMode: t.DeliveryMode, DeliveryChannelID: t.DeliveryChannelID, DeliveryPeerID: t.DeliveryPeerID}, nil
}

func (r *cronIlinkTaskRepo) GetByID(_ context.Context, id string) (*CronTaskMeta, error) {
	t, ok := r.tasks[id]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (r *cronIlinkTaskRepo) Update(_ context.Context, id string, updates map[string]any) (*CronTaskMeta, error) {
	r.updated = append(r.updated, updates)
	return r.GetByID(context.Background(), id)
}

func newCronIlinkUsecaseForTest() (*CronUsecase, *cronIlinkTaskRepo) {
	channels := &ilinkFakeChannels{byID: map[string]*ChannelMeta{
		"uuid-1":   {ID: "uuid-1", ChannelID: "wx-1", Type: ChannelTypeWeixinIlink, Enabled: true},
		"uuid-wcm": {ID: "uuid-wcm", ChannelID: "wecom-1", Type: "wecom", Enabled: true},
	}}
	roster := &pushFakeRepo{ilinkFakeRepo: &ilinkFakeRepo{}, peers: map[string]*IlinkPeer{
		"p1":      {PeerID: "p1", Allowed: true},
		"blocked": {PeerID: "blocked"},
	}}
	now := time.Now()
	peers := &fakeChannelPeerSessionRepo{rows: map[string]*ChannelPeerSession{
		peerKey("wx-1", "user:p1"):     {ChannelID: "wx-1", PeerID: "user:p1", SessionID: "s-wx", UpdatedAt: now},
		peerKey("wecom-1", "user:p2"):  {ChannelID: "wecom-1", PeerID: "user:p2", SessionID: "s-wecom", UpdatedAt: now},
		peerKey("gone-1", "user:p3"):   {ChannelID: "gone-1", PeerID: "user:p3", SessionID: "s-gone", UpdatedAt: now},
		peerKey("wx-1", "user:older"):  {ChannelID: "wx-1", PeerID: "user:older", SessionID: "s-multi", UpdatedAt: now.Add(-time.Hour)},
		peerKey("wx-1", "user:latest"): {ChannelID: "wx-1", PeerID: "user:latest", SessionID: "s-multi", UpdatedAt: now},
	}}
	tasks := &cronIlinkTaskRepo{tasks: map[string]*CronTaskMeta{
		"t-wx":   {ID: "t-wx", DeliveryMode: "channel", DeliveryChannelID: "uuid-1", DeliveryPeerID: "blocked"},
		"t-none": {ID: "t-none", DeliveryMode: "none"},
	}}
	uc := NewCronUsecase(tasks, nil, log.DefaultLogger)
	uc.SetWeixinIlink(channels, peers, roster)
	return uc, tasks
}

func TestCronCreate_IlinkDeliveryValidation(t *testing.T) {
	uc, tasks := newCronIlinkUsecaseForTest()
	ctx := context.Background()
	create := func(channelID, peer string) error {
		_, err := uc.Create(ctx, &CronTaskCreate{Name: "n", DeliveryMode: "channel", DeliveryChannelID: channelID, DeliveryPeerID: peer})
		return err
	}

	if err := create("uuid-1", ""); !isIlinkInvalidArgument(err) || !strings.Contains(err.Error(), "delivery_peer_id") {
		t.Fatalf("missing peer err = %v", err)
	}
	for _, peer := range []string{"p9", "blocked"} {
		if err := create("uuid-1", peer); !isIlinkInvalidArgument(err) || !strings.Contains(err.Error(), "目标 "+peer+" 未在白名单") {
			t.Fatalf("peer %s err = %v", peer, err)
		}
	}
	if err := create("uuid-1", strings.Repeat("字", ilinkPeerIDMaxRunes+1)); !isIlinkInvalidArgument(err) {
		t.Fatalf("long peer err = %v", err)
	}
	if len(tasks.created) != 0 {
		t.Fatalf("created = %d, want none", len(tasks.created))
	}
	if err := create("uuid-1", " p1 "); err != nil {
		t.Fatalf("allowed peer err = %v", err)
	}
	if got := tasks.created[0].DeliveryPeerID; got != "p1" {
		t.Fatalf("stored peer = %q, want trimmed", got)
	}
	if err := create("uuid-wcm", ""); err != nil {
		t.Fatalf("wecom channel err = %v", err)
	}
	if err := create("missing", ""); err != nil {
		t.Fatalf("unknown channel err = %v", err)
	}
}

func TestCronCreate_SkipsIlinkValidationWhenNotWired(t *testing.T) {
	tasks := &cronIlinkTaskRepo{}
	uc := NewCronUsecase(tasks, nil, log.DefaultLogger)
	if _, err := uc.Create(context.Background(), &CronTaskCreate{DeliveryMode: "channel", DeliveryChannelID: "uuid-1"}); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestCronUpdate_IlinkDeliveryValidation(t *testing.T) {
	uc, tasks := newCronIlinkUsecaseForTest()
	ctx := context.Background()

	if _, err := uc.Update(ctx, "t-wx", map[string]any{"enabled": false}); err != nil {
		t.Fatalf("pause with a stale peer err = %v", err)
	}
	if _, err := uc.Update(ctx, "t-wx", map[string]any{"name": "x", "delivery_peer_id": "blocked"}); !isIlinkInvalidArgument(err) {
		t.Fatalf("disallowed peer err = %v", err)
	}
	if _, err := uc.Update(ctx, "t-none", map[string]any{"delivery_mode": "channel", "delivery_channel_id": "uuid-1"}); !isIlinkInvalidArgument(err) {
		t.Fatalf("switch to weixin without peer err = %v", err)
	}
	updates := map[string]any{"delivery_peer_id": " p1 "}
	if _, err := uc.Update(ctx, "t-wx", updates); err != nil {
		t.Fatalf("allowed peer err = %v", err)
	}
	if updates["delivery_peer_id"] != "p1" {
		t.Fatalf("updates peer = %v, want trimmed", updates["delivery_peer_id"])
	}
	if _, err := uc.Update(ctx, "missing", map[string]any{"delivery_peer_id": "p1"}); err != ErrCronTaskNotFound {
		t.Fatalf("missing task err = %v", err)
	}
	if len(tasks.updated) != 2 {
		t.Fatalf("repo updates = %d, want 2", len(tasks.updated))
	}
}

func TestCronIlinkOrigin(t *testing.T) {
	uc, _ := newCronIlinkUsecaseForTest()
	ctx := context.Background()
	cases := map[string][2]string{
		"s-wx":    {"uuid-1", "p1"},
		"s-multi": {"uuid-1", "latest"},
		"s-wecom": {"", ""},
		"s-gone":  {"", ""},
		"unknown": {"", ""},
		"":        {"", ""},
	}
	for sessionID, want := range cases {
		ch, peer, err := uc.IlinkOrigin(ctx, sessionID)
		if err != nil || ch != want[0] || peer != want[1] {
			t.Errorf("IlinkOrigin(%q) = %q, %q, %v; want %v", sessionID, ch, peer, err, want)
		}
	}
}
