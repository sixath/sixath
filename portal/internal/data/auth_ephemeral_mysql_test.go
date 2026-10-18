package data

import (
	"context"
	"testing"
	"time"

	"backend/internal/data/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openAuthEphemeralTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.AuthEphemeral{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newAuthEphemeralRepoForTest(db *gorm.DB) *authEphemeralRepo {
	return &authEphemeralRepo{db: db}
}

func TestAuthEphemeralPutThenConsumeOnce(t *testing.T) {
	db := openAuthEphemeralTestDB(t)
	repo := newAuthEphemeralRepoForTest(db)
	ctx := context.Background()
	now := time.Now()
	expires := now.Add(time.Hour)

	if err := repo.Put(ctx, "state-1", "wecom_state", `{"org":"demo"}`, expires); err != nil {
		t.Fatalf("Put: %v", err)
	}

	payload, err := repo.Consume(ctx, "state-1", "wecom_state", now)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if payload != `{"org":"demo"}` {
		t.Fatalf("payload = %q, want org demo json", payload)
	}

	if _, err := repo.Consume(ctx, "state-1", "wecom_state", now); err != ErrNotFound {
		t.Fatalf("second Consume = %v, want ErrNotFound", err)
	}
}

func TestAuthEphemeralConsumeExpired(t *testing.T) {
	db := openAuthEphemeralTestDB(t)
	repo := newAuthEphemeralRepoForTest(db)
	ctx := context.Background()
	now := time.Now()
	expires := now.Add(-time.Minute)

	if err := repo.Put(ctx, "state-expired", "wecom_state", `{}`, expires); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := repo.Consume(ctx, "state-expired", "wecom_state", now); err != ErrNotFound {
		t.Fatalf("Consume expired = %v, want ErrNotFound", err)
	}
}

func TestAuthEphemeralRepo_GetDoesNotConsume(t *testing.T) {
	db := openAuthEphemeralTestDB(t)
	repo := newAuthEphemeralRepoForTest(db)
	ctx := context.Background()
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

	if err := repo.Put(ctx, "qr-key", "ilink_qrcode", `{"channel_uuid":"c1"}`, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	for i := 0; i < 2; i++ {
		payload, err := repo.Get(ctx, "qr-key", "ilink_qrcode", now)
		if err != nil || payload != `{"channel_uuid":"c1"}` {
			t.Fatalf("Get #%d = %q, %v", i, payload, err)
		}
	}
	if _, err := repo.Get(ctx, "qr-key", "wecom_state", now); err != ErrNotFound {
		t.Fatalf("wrong kind err = %v, want ErrNotFound", err)
	}
	if _, err := repo.Get(ctx, "qr-key", "ilink_qrcode", now.Add(6*time.Minute)); err != ErrNotFound {
		t.Fatalf("expired err = %v, want ErrNotFound", err)
	}
	if _, err := repo.Consume(ctx, "qr-key", "ilink_qrcode", now); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if _, err := repo.Get(ctx, "qr-key", "ilink_qrcode", now); err != ErrNotFound {
		t.Fatalf("consumed err = %v, want ErrNotFound", err)
	}
}
