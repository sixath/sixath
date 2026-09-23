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
