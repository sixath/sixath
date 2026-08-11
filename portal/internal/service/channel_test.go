package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	channelv1 "backend/api/channel/v1"
	"backend/internal/biz"
	pkgErrors "backend/internal/pkg/errors"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/encoding/protojson"
)

type fakeChannelRuntimeRepo struct {
	byID map[string]*biz.RuntimeStatusRow
	err  error
}

func (f *fakeChannelRuntimeRepo) Get(_ context.Context, channelID string) (*biz.RuntimeStatusRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	row, ok := f.byID[channelID]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *row
	return &cp, nil
}

func (f *fakeChannelRuntimeRepo) Upsert(context.Context, string, biz.RuntimeStatusPatch) error {
	return nil
}

func TestChannelMetaToReply_RedactsSecrets(t *testing.T) {
	ch := &biz.ChannelMeta{
		ID:         "uuid-1",
		ChannelID:  "bot-a",
		Type:       "wecom_bot",
		Enabled:    true,
		BotID:      "bid",
		BotSecret:  "super-secret-bot",
		CorpID:     "corp",
		CorpSecret: "super-secret-corp",
		BotNames:   []string{"alice"},
		WSURL:      "wss://example",
		CreatedAt:  time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
	}
	r := channelMetaToReply(ch)
	if !r.SecretSet {
		t.Fatal("secret_set = false, want true")
	}
	if r.BotId != "bid" || r.CorpId != "corp" {
		t.Fatalf("bot_id/corp_id = %q/%q", r.BotId, r.CorpId)
	}

	b, err := protojson.Marshal(r)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	raw := string(b)
	if strings.Contains(raw, "super-secret-bot") || strings.Contains(raw, "super-secret-corp") {
		t.Fatalf("reply JSON leaked plaintext secret: %s", raw)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for _, key := range []string{"secret", "bot_secret", "corp_secret"} {
		if _, ok := m[key]; ok {
			t.Fatalf("reply JSON contains forbidden key %q: %s", key, raw)
		}
	}
}

func TestChannelToReply_AttachesRuntimeStatus(t *testing.T) {
	hb := time.Now().UTC().Add(-20 * time.Second)
	svc := &ChannelService{
		runtimeRepo: &fakeChannelRuntimeRepo{byID: map[string]*biz.RuntimeStatusRow{
			"bot-a": {
				ChannelID:       "bot-a",
				State:           "connected",
				LastHeartbeatAt: hb,
			},
		}},
		log: log.NewHelper(log.DefaultLogger),
	}
	ch := &biz.ChannelMeta{
		ID:        "uuid-1",
		ChannelID: "bot-a",
		Type:      "wecom_bot",
		Enabled:   true,
		BotSecret: "x",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	r := svc.channelToReply(context.Background(), ch)
	if r.RuntimeStatus == nil {
		t.Fatal("runtime_status = nil, want connected view")
	}
	if r.RuntimeStatus.State != "connected" {
		t.Fatalf("runtime_status.state = %q, want connected", r.RuntimeStatus.State)
	}
	if r.RuntimeStatus.LastHeartbeatAt == "" {
		t.Fatal("last_heartbeat_at empty")
	}
}

func TestChannelToReply_NonWecomBotOmitsRuntimeStatus(t *testing.T) {
	svc := &ChannelService{
		runtimeRepo: &fakeChannelRuntimeRepo{byID: map[string]*biz.RuntimeStatusRow{
			"wh-1": {ChannelID: "wh-1", State: "connected", LastHeartbeatAt: time.Now()},
		}},
		log: log.NewHelper(log.DefaultLogger),
	}
	ch := &biz.ChannelMeta{
		ID:        "uuid-2",
		ChannelID: "wh-1",
		Type:      "webhook",
		Enabled:   true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	r := svc.channelToReply(context.Background(), ch)
	if r.RuntimeStatus != nil {
		t.Fatalf("runtime_status = %#v, want nil", r.RuntimeStatus)
	}
}

func TestCreateChannel_MapsGatewayFieldsWithoutLeakingSecrets(t *testing.T) {
	repo := &channelCreateRepo{}
	uc := biz.NewChannelUsecase(repo, nil, log.DefaultLogger)
	svc := NewChannelService(uc, &fakeChannelRuntimeRepo{byID: map[string]*biz.RuntimeStatusRow{}}, nil, log.DefaultLogger)

	reply, err := svc.CreateChannel(context.Background(), &channelv1.CreateChannelRequest{
		ChannelId:        "bot-create",
		Type:             "wecom_bot",
		Enabled:          true,
		BotId:            "bid",
		Secret:           "plain-bot-secret",
		BotNames:         []string{"n1"},
		WsUrl:            "wss://ex",
		CorpId:           "corp",
		CorpSecret:       "plain-corp-secret",
		DefaultReplyMode: "async",
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if repo.last == nil {
		t.Fatal("repo.Create not called")
	}
	if repo.last.BotSecret != "plain-bot-secret" || repo.last.CorpSecret != "plain-corp-secret" {
		t.Fatalf("create mapping secrets = %q/%q", repo.last.BotSecret, repo.last.CorpSecret)
	}
	if !reply.SecretSet {
		t.Fatal("secret_set = false, want true")
	}
	b, err := protojson.Marshal(reply)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	raw := string(b)
	if strings.Contains(raw, "plain-bot-secret") || strings.Contains(raw, "plain-corp-secret") {
		t.Fatalf("CreateChannel reply leaked secrets: %s", raw)
	}
	if reply.RuntimeStatus == nil || reply.RuntimeStatus.State != "unknown" {
		t.Fatalf("runtime_status = %#v, want unknown", reply.RuntimeStatus)
	}
}

// channelCreateRepo is a minimal ChannelRepo for Create mapping tests.
type channelCreateRepo struct {
	last *biz.ChannelCreate
}

func (r *channelCreateRepo) Create(_ context.Context, ch *biz.ChannelCreate) (*biz.ChannelMeta, error) {
	cp := *ch
	r.last = &cp
	return &biz.ChannelMeta{
		ID:               "uuid-new",
		ChannelID:        ch.ChannelID,
		Type:             ch.Type,
		DefaultAgent:     ch.DefaultAgent,
		AllowedAgents:    ch.AllowedAgents,
		Enabled:          ch.Enabled,
		BotID:            ch.BotID,
		BotSecret:        ch.BotSecret,
		BotNames:         ch.BotNames,
		WSURL:            ch.WSURL,
		CorpID:           ch.CorpID,
		CorpSecret:       ch.CorpSecret,
		DefaultReplyMode: ch.DefaultReplyMode,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}, nil
}
func (r *channelCreateRepo) GetByID(context.Context, string) (*biz.ChannelMeta, error) {
	return nil, pkgErrors.ErrNotFound
}
func (r *channelCreateRepo) GetByChannelID(context.Context, string) (*biz.ChannelMeta, error) {
	return nil, pkgErrors.ErrNotFound
}
func (r *channelCreateRepo) GetWecomByDefaultAgent(context.Context, string) (*biz.ChannelMeta, error) {
	return nil, pkgErrors.ErrNotFound
}
func (r *channelCreateRepo) List(context.Context, int32, int32, string, *bool) ([]*biz.ChannelMeta, int, error) {
	return nil, 0, nil
}
func (r *channelCreateRepo) ListGatewayChannels(context.Context) ([]*biz.ChannelMeta, error) {
	return nil, nil
}
func (r *channelCreateRepo) Update(context.Context, string, map[string]any) (*biz.ChannelMeta, error) {
	return nil, pkgErrors.ErrNotFound
}
func (r *channelCreateRepo) Delete(context.Context, string) error { return pkgErrors.ErrNotFound }
