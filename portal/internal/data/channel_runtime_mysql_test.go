package data

import (
	"context"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openChannelRuntimeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.ChannelRuntimeStatus{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newChannelRuntimeRepoForTest(db *gorm.DB) *channelRuntimeRepo {
	return &channelRuntimeRepo{db: db}
}

func TestUpsertStatus_ConnectedClearsError(t *testing.T) {
	db := openChannelRuntimeTestDB(t)
	repo := newChannelRuntimeRepoForTest(db)
	ctx := context.Background()

	errMsg := "boom"
	attempt := 3
	inMs := 8000
	if err := repo.Upsert(ctx, "ch-1", biz.RuntimeStatusPatch{
		State:            "disconnected",
		LastError:        &errMsg,
		ReconnectAttempt: &attempt,
		ReconnectInMs:    &inMs,
	}); err != nil {
		t.Fatalf("seed Upsert: %v", err)
	}

	if err := repo.Upsert(ctx, "ch-1", biz.RuntimeStatusPatch{State: "connected"}); err != nil {
		t.Fatalf("connected Upsert: %v", err)
	}

	got, err := repo.Get(ctx, "ch-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != "connected" {
		t.Fatalf("state = %q, want connected", got.State)
	}
	if got.LastError != "" {
		t.Fatalf("last_error = %q, want empty", got.LastError)
	}
	if got.ReconnectAttempt != 0 || got.ReconnectInMs != 0 {
		t.Fatalf("reconnect = %d/%dms, want 0/0", got.ReconnectAttempt, got.ReconnectInMs)
	}
	if got.LastHeartbeatAt.IsZero() {
		t.Fatal("last_heartbeat_at should be refreshed")
	}
}

func TestUpsertStatus_OmitPreserves(t *testing.T) {
	db := openChannelRuntimeTestDB(t)
	repo := newChannelRuntimeRepoForTest(db)
	ctx := context.Background()

	errMsg := "still-there"
	attempt := 2
	inMs := 5000
	gw := "gw-old"
	if err := repo.Upsert(ctx, "ch-2", biz.RuntimeStatusPatch{
		State:             "reconnecting",
		LastError:         &errMsg,
		ReconnectAttempt:  &attempt,
		ReconnectInMs:     &inMs,
		GatewayInstanceID: &gw,
	}); err != nil {
		t.Fatalf("seed Upsert: %v", err)
	}

	before, err := repo.Get(ctx, "ch-2")
	if err != nil {
		t.Fatalf("Get before: %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	if err := repo.Upsert(ctx, "ch-2", biz.RuntimeStatusPatch{State: "reconnecting"}); err != nil {
		t.Fatalf("omit Upsert: %v", err)
	}

	got, err := repo.Get(ctx, "ch-2")
	if err != nil {
		t.Fatalf("Get after: %v", err)
	}
	if got.LastError != errMsg {
		t.Fatalf("last_error = %q, want %q", got.LastError, errMsg)
	}
	if got.ReconnectAttempt != attempt || got.ReconnectInMs != inMs {
		t.Fatalf("reconnect = %d/%dms, want %d/%dms", got.ReconnectAttempt, got.ReconnectInMs, attempt, inMs)
	}
	if got.GatewayInstanceID != gw {
		t.Fatalf("gateway_instance_id = %q, want %q", got.GatewayInstanceID, gw)
	}
	if !got.LastHeartbeatAt.After(before.LastHeartbeatAt) {
		t.Fatalf("last_heartbeat_at not refreshed: before=%v after=%v", before.LastHeartbeatAt, got.LastHeartbeatAt)
	}
}
