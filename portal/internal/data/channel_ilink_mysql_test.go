package data

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
)

func newIlinkRepoForTest(db *gorm.DB) *weixinIlinkRepo {
	return &weixinIlinkRepo{db: db, log: log.NewHelper(log.DefaultLogger)}
}

func seedIlinkChannel(t *testing.T, db *gorm.DB) *biz.ChannelMeta {
	t.Helper()
	ch, err := newChannelRepoForTest(db).Create(context.Background(), &biz.ChannelCreate{ChannelID: "wx-1", Type: "weixin_ilink", Enabled: true})
	if err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	return ch
}

func TestIlinkRepo_ConfirmLoginBumpsGenerationAndResetsState(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newIlinkRepoForTest(db)
	ctx := context.Background()
	ch := seedIlinkChannel(t, db)
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

	if _, err := repo.UpsertPeerReport(ctx, ch.ID, 0, biz.IlinkPeerReport{PeerID: "p1", ContextToken: "old", Preview: "hi", At: now}); err != nil {
		t.Fatalf("UpsertPeerReport: %v", err)
	}
	if err := repo.PutCursor(ctx, ch.ID, biz.IlinkCursor{Generation: 0, GetUpdatesBuf: "buf"}, now); err != nil {
		t.Fatalf("PutCursor: %v", err)
	}
	if applied, err := repo.MarkSessionExpired(ctx, ch.ID, 0, now); err != nil || !applied {
		t.Fatalf("MarkSessionExpired = %v, %v", applied, err)
	}

	meta, err := repo.ConfirmLogin(ctx, ch.ID, biz.IlinkLoginResult{BotToken: "bt", BotID: "b@im.bot", OwnerUserID: "owner@im.wechat"}, now)
	if err != nil {
		t.Fatalf("ConfirmLogin: %v", err)
	}
	if meta.IlinkGeneration != 1 || meta.IlinkBotToken != "bt" || meta.IlinkBotID != "b@im.bot" || meta.IlinkOwnerUserID != "owner@im.wechat" {
		t.Fatalf("meta = %+v", meta)
	}
	if meta.IlinkSessionExpiredAt != nil {
		t.Fatalf("expiry not cleared: %v", meta.IlinkSessionExpiredAt)
	}
	cur, err := repo.GetCursor(ctx, ch.ID)
	if err != nil || cur.GetUpdatesBuf != "" {
		t.Fatalf("cursor = %+v, %v", cur, err)
	}
	peers, err := repo.ListPeers(ctx, ch.ID)
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	byID := map[string]*biz.IlinkPeer{}
	for _, p := range peers {
		byID[p.PeerID] = p
	}
	if p1 := byID["p1"]; p1 == nil || p1.ContextToken != "" || p1.ContextTokenAt != nil || p1.LastPreview != "hi" {
		t.Fatalf("p1 = %+v", byID["p1"])
	}
	if owner := byID["owner@im.wechat"]; owner == nil || !owner.Allowed {
		t.Fatalf("owner = %+v", byID["owner@im.wechat"])
	}
	allowed, err := repo.ListAllowedPeerIDs(ctx, ch.ID)
	if err != nil || len(allowed) != 1 || allowed[0] != "owner@im.wechat" {
		t.Fatalf("allowed = %v, %v", allowed, err)
	}
}

func TestIlinkRepo_ConfirmLoginUnknownChannel(t *testing.T) {
	db := openChannelTestDB(t)
	_, err := newIlinkRepoForTest(db).ConfirmLogin(context.Background(), "missing", biz.IlinkLoginResult{BotToken: "bt"}, time.Now())
	if err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestIlinkRepo_UpsertPeerReportGenerationMismatch(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newIlinkRepoForTest(db)
	ch := seedIlinkChannel(t, db)

	_, err := repo.UpsertPeerReport(context.Background(), ch.ID, 5, biz.IlinkPeerReport{PeerID: "p1", At: time.Now()})
	if !errors.Is(err, biz.ErrIlinkGenerationMismatch) {
		t.Fatalf("err = %v, want generation mismatch", err)
	}
}

func TestIlinkRepo_UpsertPeerReportKeepsTokenWhenEmpty(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newIlinkRepoForTest(db)
	ctx := context.Background()
	ch := seedIlinkChannel(t, db)
	t1 := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

	first, err := repo.UpsertPeerReport(ctx, ch.ID, 0, biz.IlinkPeerReport{PeerID: "p1", ContextToken: "tok-1", Preview: "first", At: t1})
	if err != nil {
		t.Fatalf("first report: %v", err)
	}
	if first.Allowed {
		t.Fatal("new peer must start not allowed")
	}
	second, err := repo.UpsertPeerReport(ctx, ch.ID, 0, biz.IlinkPeerReport{PeerID: "p1", Preview: "second", At: t1.Add(time.Minute)})
	if err != nil {
		t.Fatalf("second report: %v", err)
	}
	if second.ContextToken != "tok-1" || second.LastPreview != "second" {
		t.Fatalf("second = %+v", second)
	}
	if second.LastInboundAt == nil || !second.LastInboundAt.Equal(t1.Add(time.Minute)) {
		t.Fatalf("LastInboundAt = %v", second.LastInboundAt)
	}
}

func TestIlinkRepo_PutCursorCAS(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newIlinkRepoForTest(db)
	ctx := context.Background()
	ch := seedIlinkChannel(t, db)
	now := time.Now()

	if err := repo.PutCursor(ctx, ch.ID, biz.IlinkCursor{Generation: 0, GetUpdatesBuf: "a"}, now); err != nil {
		t.Fatalf("PutCursor gen0: %v", err)
	}
	if err := repo.PutCursor(ctx, ch.ID, biz.IlinkCursor{Generation: 0, GetUpdatesBuf: "b"}, now); err != nil {
		t.Fatalf("PutCursor overwrite: %v", err)
	}
	cur, err := repo.GetCursor(ctx, ch.ID)
	if err != nil || cur.GetUpdatesBuf != "b" || cur.Generation != 0 {
		t.Fatalf("cursor = %+v, %v", cur, err)
	}
	if err := repo.PutCursor(ctx, ch.ID, biz.IlinkCursor{Generation: 1, GetUpdatesBuf: "c"}, now); !errors.Is(err, biz.ErrIlinkGenerationMismatch) {
		t.Fatalf("stale PutCursor err = %v", err)
	}
}

func TestIlinkRepo_MarkSessionExpiredIgnoresStaleGeneration(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newIlinkRepoForTest(db)
	ctx := context.Background()
	ch := seedIlinkChannel(t, db)

	applied, err := repo.MarkSessionExpired(ctx, ch.ID, 7, time.Now())
	if err != nil || applied {
		t.Fatalf("applied=%v err=%v, want not applied", applied, err)
	}
	got, err := newChannelRepoForTest(db).GetByID(ctx, ch.ID)
	if err != nil || got.IlinkSessionExpiredAt != nil {
		t.Fatalf("channel = %+v, %v", got, err)
	}
}

func TestIlinkRepo_UpdateAndDeletePeer(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newIlinkRepoForTest(db)
	ctx := context.Background()
	ch := seedIlinkChannel(t, db)
	yes := true
	remark := "张三"

	if _, err := repo.UpdatePeer(ctx, ch.ID, "p1", biz.IlinkPeerPatch{Allowed: &yes}); err != ErrNotFound {
		t.Fatalf("update missing err = %v", err)
	}
	if _, err := repo.UpsertPeerReport(ctx, ch.ID, 0, biz.IlinkPeerReport{PeerID: "p1", At: time.Now()}); err != nil {
		t.Fatalf("report: %v", err)
	}
	p, err := repo.UpdatePeer(ctx, ch.ID, "p1", biz.IlinkPeerPatch{Allowed: &yes, Remark: &remark})
	if err != nil || !p.Allowed || p.Remark != "张三" {
		t.Fatalf("UpdatePeer = %+v, %v", p, err)
	}
	if err := repo.DeletePeer(ctx, ch.ID, "p1"); err != nil {
		t.Fatalf("DeletePeer: %v", err)
	}
	if err := repo.DeletePeer(ctx, ch.ID, "p1"); err != ErrNotFound {
		t.Fatalf("second DeletePeer err = %v", err)
	}
	var count int64
	db.Model(&model.ChannelIlinkPeer{}).Where("channel_uuid = ?", ch.ID).Count(&count)
	if count != 0 {
		t.Fatalf("rows left = %d", count)
	}
}
