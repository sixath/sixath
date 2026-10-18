package data

import (
	"context"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openChannelTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Channel{}, &model.ChannelIlinkPeer{}, &model.ChannelIlinkState{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newChannelRepoForTest(db *gorm.DB) *channelRepo {
	return &channelRepo{db: db, log: log.NewHelper(log.DefaultLogger)}
}

func TestChannelRepo_CreateUpdateGatewayFields(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newChannelRepoForTest(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, &biz.ChannelCreate{
		ChannelID:        "gw-1",
		Type:             "wecom_bot",
		Enabled:          true,
		BotID:            "bot-1",
		BotSecret:        "sec-1",
		BotNames:         []string{"助手"},
		WSURL:            "wss://example.com/ws",
		CorpID:           "corp-1",
		CorpSecret:       "corp-sec",
		DefaultReplyMode: "",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.BotID != "bot-1" || created.BotSecret != "sec-1" || created.WSURL == "" {
		t.Fatalf("Create mapping incomplete: %+v", created)
	}

	updated, err := repo.Update(ctx, created.ID, map[string]any{
		"bot_secret":         "sec-2",
		"default_reply_mode": "async",
		"bot_names":          []string{"A", "B"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.BotSecret != "sec-2" {
		t.Fatalf("BotSecret = %q, want sec-2", updated.BotSecret)
	}
	if updated.DefaultReplyMode != "async" {
		t.Fatalf("DefaultReplyMode = %q, want async", updated.DefaultReplyMode)
	}
	if len(updated.BotNames) != 2 {
		t.Fatalf("BotNames = %v, want 2 items", updated.BotNames)
	}
}

func TestChannelRepo_CreateDisabledPersistsFalse(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newChannelRepoForTest(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, &biz.ChannelCreate{
		ChannelID: "disabled-1",
		Type:      "webhook",
		Enabled:   false,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Enabled {
		t.Fatal("Create returned Enabled=true, want false")
	}

	var raw int
	if err := db.Raw("SELECT enabled FROM channels WHERE id = ?", created.ID).Scan(&raw).Error; err != nil {
		t.Fatalf("raw select: %v", err)
	}
	if raw != 0 {
		t.Fatalf("raw enabled = %d, want 0 (single Create path, no orphan enabled=true)", raw)
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Enabled {
		t.Fatal("GetByID Enabled=true, want false")
	}
}

func TestChannelRepo_UpdateBotNamesFromAnySlice(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newChannelRepoForTest(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, &biz.ChannelCreate{
		ChannelID: "names-any",
		Type:      "wecom_bot",
		Enabled:   true,
		BotID:     "b1",
		BotSecret: "s1",
		BotNames:  []string{"old"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := repo.Update(ctx, created.ID, map[string]any{
		"bot_names": []any{"N1", "N2"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(updated.BotNames) != 2 || updated.BotNames[0] != "N1" || updated.BotNames[1] != "N2" {
		t.Fatalf("BotNames = %v, want [N1 N2]", updated.BotNames)
	}
}

func TestChannelRepo_ListGatewayChannelsIncludesDisabled(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newChannelRepoForTest(db)
	ctx := context.Background()

	seeds := []*biz.ChannelCreate{
		{ChannelID: "wh-on", Type: "webhook", Enabled: true, DefaultReplyMode: "sync", WebhookSecret: "s1"},
		{ChannelID: "wb-off", Type: "wecom_bot", Enabled: false, BotID: "b1", BotSecret: "plain-secret"},
		{ChannelID: "web-only", Type: "web", Enabled: true},
		{ChannelID: "wecom-group", Type: "wecom", Enabled: true, WebhookURL: "https://qyapi.example/"},
	}
	for _, s := range seeds {
		if _, err := repo.Create(ctx, s); err != nil {
			t.Fatalf("Create %s: %v", s.ChannelID, err)
		}
	}

	list, err := repo.ListGatewayChannels(ctx)
	if err != nil {
		t.Fatalf("ListGatewayChannels: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
	byID := map[string]*biz.ChannelMeta{}
	for _, ch := range list {
		byID[ch.ChannelID] = ch
	}
	off := byID["wb-off"]
	if off == nil {
		t.Fatal("expected disabled wecom_bot in list")
	}
	if off.Enabled {
		t.Fatal("wb-off should be disabled")
	}
	if off.BotSecret != "plain-secret" {
		t.Fatalf("BotSecret = %q, want plaintext plain-secret", off.BotSecret)
	}
	if byID["wh-on"] == nil {
		t.Fatal("expected webhook in list")
	}
	if byID["web-only"] != nil || byID["wecom-group"] != nil {
		t.Fatalf("unexpected types in list: %+v", byID)
	}
}

func TestChannelRepo_ListGatewayChannelsIncludesWeixinIlink(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newChannelRepoForTest(db)
	ctx := context.Background()

	ch, err := repo.Create(ctx, &biz.ChannelCreate{ChannelID: "wx-1", Type: "weixin_ilink", Enabled: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	expired := time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC)
	if err := db.Model(&model.Channel{}).Where("id = ?", ch.ID).Updates(map[string]any{
		"ilink_bot_token": "bt", "ilink_bot_id": "b@im.bot", "ilink_owner_user_id": "u@im.wechat",
		"ilink_generation": 2, "ilink_session_expired_at": expired,
	}).Error; err != nil {
		t.Fatalf("seed ilink columns: %v", err)
	}

	list, err := repo.ListGatewayChannels(ctx)
	if err != nil {
		t.Fatalf("ListGatewayChannels: %v", err)
	}
	if len(list) != 1 || list[0].Type != "weixin_ilink" {
		t.Fatalf("list = %+v", list)
	}
	got := list[0]
	if got.IlinkBotToken != "bt" || got.IlinkBotID != "b@im.bot" || got.IlinkOwnerUserID != "u@im.wechat" || got.IlinkGeneration != 2 {
		t.Fatalf("ilink fields = %+v", got)
	}
	if got.IlinkSessionExpiredAt == nil || !got.IlinkSessionExpiredAt.Equal(expired) {
		t.Fatalf("IlinkSessionExpiredAt = %v", got.IlinkSessionExpiredAt)
	}
}

func TestChannelRepo_UpdateIgnoresIlinkCredentialKeys(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newChannelRepoForTest(db)
	ctx := context.Background()

	ch, err := repo.Create(ctx, &biz.ChannelCreate{ChannelID: "wx-1", Type: "weixin_ilink", Enabled: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := repo.Update(ctx, ch.ID, map[string]any{
		"ilink_bot_token": "injected", "ilink_generation": 99, "enabled": false,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.IlinkBotToken != "" || updated.IlinkGeneration != 0 || updated.Enabled {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestChannelRepo_DeleteCascadesIlinkRows(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newChannelRepoForTest(db)
	ctx := context.Background()

	ch, err := repo.Create(ctx, &biz.ChannelCreate{ChannelID: "wx-1", Type: "weixin_ilink", Enabled: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := time.Now()
	if err := db.Create(&model.ChannelIlinkPeer{ChannelUUID: ch.ID, PeerID: "p1", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed peer: %v", err)
	}
	if err := db.Create(&model.ChannelIlinkState{ChannelUUID: ch.ID, GetUpdatesBuf: "buf", UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed state: %v", err)
	}

	if err := repo.Delete(ctx, ch.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var peers, states int64
	db.Model(&model.ChannelIlinkPeer{}).Where("channel_uuid = ?", ch.ID).Count(&peers)
	db.Model(&model.ChannelIlinkState{}).Where("channel_uuid = ?", ch.ID).Count(&states)
	if peers != 0 || states != 0 {
		t.Fatalf("leftover peers=%d states=%d", peers, states)
	}
	if err := repo.Delete(ctx, ch.ID); err != ErrNotFound {
		t.Fatalf("second Delete err = %v, want ErrNotFound", err)
	}
}

func TestChannelRepo_ListIlinkByAgent(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newChannelRepoForTest(db)
	ctx := context.Background()

	for _, in := range []*biz.ChannelCreate{
		{ChannelID: "wx-b", Type: "weixin_ilink", DefaultAgent: "a1", Enabled: true},
		{ChannelID: "wx-a", Type: "weixin_ilink", DefaultAgent: "a2", AllowedAgents: []string{"a2", "a1"}, Enabled: true},
		{ChannelID: "wx-off", Type: "weixin_ilink", DefaultAgent: "a1", Enabled: false},
		{ChannelID: "wx-other", Type: "weixin_ilink", DefaultAgent: "a3", Enabled: true},
		{ChannelID: "wecom-1", Type: "wecom", DefaultAgent: "a1", Enabled: true},
	} {
		if _, err := repo.Create(ctx, in); err != nil {
			t.Fatalf("Create %s: %v", in.ChannelID, err)
		}
	}

	got, err := repo.ListIlinkByAgent(ctx, "a1")
	if err != nil {
		t.Fatalf("ListIlinkByAgent: %v", err)
	}
	if len(got) != 2 || got[0].ChannelID != "wx-a" || got[1].ChannelID != "wx-b" {
		t.Fatalf("channels = %+v", got)
	}
	if none, err := repo.ListIlinkByAgent(ctx, "  "); err != nil || len(none) != 0 {
		t.Fatalf("blank agent = %+v, %v", none, err)
	}
}
