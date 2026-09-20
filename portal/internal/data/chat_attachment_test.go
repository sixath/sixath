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

func openAttachmentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.ChatAttachment{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newAttachmentRepoForTest(db *gorm.DB) biz.AttachmentRepo {
	return NewChatAttachmentRepo(&Data{db: db}, log.DefaultLogger)
}

func TestChatAttachmentRepo_CreateGetCountListDelete(t *testing.T) {
	db := openAttachmentTestDB(t)
	repo := newAttachmentRepoForTest(db)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	a := &biz.ChatAttachment{
		ID:           "att-1",
		SessionID:    "sess-1",
		Kind:         "image",
		Mime:         "image/png",
		Name:         "shot.png",
		Size:         128,
		RelativePath: "sessions/sess-1/uploads/att-1_shot.png",
		CreatedAt:    now,
	}
	if err := repo.Create(ctx, a); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, "sess-1", "att-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "shot.png" || got.Kind != "image" || got.Size != 128 {
		t.Fatalf("Get = %+v", got)
	}
	if !got.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}

	// Wrong session must miss.
	if _, err := repo.Get(ctx, "other", "att-1"); err != ErrNotFound {
		t.Fatalf("Get wrong session = %v, want ErrNotFound", err)
	}

	n, err := repo.CountBySession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("CountBySession: %v", err)
	}
	if n != 1 {
		t.Fatalf("Count = %d, want 1", n)
	}

	a2 := &biz.ChatAttachment{
		ID: "att-2", SessionID: "sess-1", Kind: "text", Mime: "text/plain",
		Name: "a.txt", Size: 4, RelativePath: "sessions/sess-1/uploads/att-2_a.txt",
		CreatedAt: now.Add(time.Second),
	}
	if err := repo.Create(ctx, a2); err != nil {
		t.Fatalf("Create a2: %v", err)
	}

	list, err := repo.ListByIDs(ctx, "sess-1", []string{"att-2", "missing", "att-1"})
	if err != nil {
		t.Fatalf("ListByIDs: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
	ids := map[string]bool{}
	for _, item := range list {
		ids[item.ID] = true
	}
	if !ids["att-1"] || !ids["att-2"] {
		t.Fatalf("List ids = %v", ids)
	}

	n, err = repo.CountBySession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("Count after 2: %v", err)
	}
	if n != 2 {
		t.Fatalf("Count = %d, want 2", n)
	}

	if err := repo.Delete(ctx, "sess-1", "att-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, "sess-1", "att-1"); err != ErrNotFound {
		t.Fatalf("Get after delete = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, "sess-1", "att-1"); err != ErrNotFound {
		t.Fatalf("Delete missing = %v, want ErrNotFound", err)
	}

	n, err = repo.CountBySession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("Count after delete: %v", err)
	}
	if n != 1 {
		t.Fatalf("Count after delete = %d, want 1", n)
	}
}

func TestChatAttachmentRepo_ListByIDsEmpty(t *testing.T) {
	db := openAttachmentTestDB(t)
	repo := newAttachmentRepoForTest(db)
	list, err := repo.ListByIDs(context.Background(), "sess-1", nil)
	if err != nil {
		t.Fatalf("ListByIDs empty: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List = %v, want empty", list)
	}
}
