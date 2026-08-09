package data

import (
	"context"
	"testing"
	"time"

	"backend/internal/data/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openChatRewindTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.ChatSession{}, &model.ChatMessage{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestListBySession_SkipsInactive(t *testing.T) {
	db := openChatRewindTestDB(t)
	sessRepo := &chatSessionRepo{db: db}
	msgRepo := &chatMessageRepo{db: db}
	ctx := context.Background()

	sess, err := sessRepo.Create(ctx, "u1", "a1", "rewind-test", "")
	if err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	ids := make([]string, 0, 3)
	for i, content := range []string{"m0", "m1-anchor", "m2"} {
		m := &model.ChatMessage{
			ID:        "msg-" + content,
			SessionID: sess.ID,
			Role:      "user",
			Content:   content,
			Active:    true,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := db.Create(m).Error; err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
	}

	anchorID := ids[1]
	deactivated, err := msgRepo.SoftDeactivateAfter(ctx, sess.ID, base.Add(time.Second), anchorID)
	if err != nil {
		t.Fatalf("SoftDeactivateAfter: %v", err)
	}
	if len(deactivated) != 2 {
		t.Fatalf("deactivated=%v want 2 (anchor+later)", deactivated)
	}

	list, err := msgRepo.ListBySession(ctx, sess.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Content != "m0" {
		t.Fatalf("list=%+v want only m0", list)
	}

	got, err := msgRepo.GetByID(ctx, anchorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Active {
		t.Fatal("anchor should be inactive but GetByID still returns it")
	}
}

func TestBumpRewindCount(t *testing.T) {
	db := openChatRewindTestDB(t)
	sessRepo := &chatSessionRepo{db: db}
	ctx := context.Background()
	sess, err := sessRepo.Create(ctx, "u1", "a1", "c", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := sessRepo.BumpRewindCount(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := sessRepo.BumpRewindCount(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	got, err := sessRepo.GetByID(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RewindCount != 2 {
		t.Fatalf("RewindCount=%d want 2", got.RewindCount)
	}
}
