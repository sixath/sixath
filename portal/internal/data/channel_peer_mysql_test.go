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

func newPeerSessionRepoForTest(t *testing.T) *channelPeerSessionRepo {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.ChannelPeerSession{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &channelPeerSessionRepo{db: db, log: log.NewHelper(log.DefaultLogger)}
}

func TestChannelPeerSessionRepo_GetBySessionID(t *testing.T) {
	repo := newPeerSessionRepoForTest(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

	if _, err := repo.GetBySessionID(ctx, "s1"); err != ErrNotFound {
		t.Fatalf("missing err = %v, want ErrNotFound", err)
	}
	for _, row := range []*biz.ChannelPeerSession{
		{ChannelID: "wx-1", PeerID: "user:old", SessionID: "s1", AgentID: "a1", CreatedAt: t0, UpdatedAt: t0},
		{ChannelID: "wx-2", PeerID: "user:new", SessionID: "s1", AgentID: "a1", CreatedAt: t0, UpdatedAt: t0.Add(time.Minute)},
		{ChannelID: "wx-1", PeerID: "user:x", SessionID: "s2", AgentID: "a1", CreatedAt: t0, UpdatedAt: t0.Add(time.Hour)},
	} {
		if err := repo.Create(ctx, row); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	got, err := repo.GetBySessionID(ctx, "s1")
	if err != nil || got.ChannelID != "wx-2" || got.PeerID != "user:new" {
		t.Fatalf("GetBySessionID = %+v, %v", got, err)
	}
}
