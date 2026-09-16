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

func TestListActiveOrdered_NoLimitAndIdTiebreak(t *testing.T) {
	db := openChatRewindTestDB(t)
	sessRepo := &chatSessionRepo{db: db}
	msgRepo := &chatMessageRepo{db: db}
	ctx := context.Background()
	sess, _ := sessRepo.Create(ctx, "u1", "a1", "t", "")
	base := time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)
	for i, id := range []string{"m-b", "m-a"} {
		_ = db.Create(&model.ChatMessage{
			ID: id, SessionID: sess.ID, Role: "user", Content: id, Active: true,
			CreatedAt: base,
		}).Error
		_ = i
	}
	_ = db.Create(&model.ChatMessage{
		ID: "m-late", SessionID: sess.ID, Role: "assistant", Content: "late", Active: true,
		CreatedAt: base.Add(time.Second),
	}).Error
	_ = db.Create(&model.ChatMessage{
		ID: "m-dead", SessionID: sess.ID, Role: "user", Content: "x", Active: false,
		CreatedAt: base.Add(2 * time.Second),
	}).Error
	// GORM Create skips bool zero values when the column has default:1, so force inactive.
	_ = db.Model(&model.ChatMessage{}).Where("id = ?", "m-dead").Update("active", false).Error
	list, err := msgRepo.ListActiveOrdered(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("len=%d want 3 (inactive excluded)", len(list))
	}
	if list[0].ID != "m-a" || list[1].ID != "m-b" || list[2].ID != "m-late" {
		t.Fatalf("order %+v", []string{list[0].ID, list[1].ID, list[2].ID})
	}
}

func TestInsertClone_PreservesCreatedAt(t *testing.T) {
	db := openChatRewindTestDB(t)
	sessRepo := &chatSessionRepo{db: db}
	msgRepo := &chatMessageRepo{db: db}
	ctx := context.Background()
	parent, _ := sessRepo.Create(ctx, "u1", "a1", "p", "")
	child, _ := sessRepo.Create(ctx, "u1", "a1", "c", parent.ID)
	srcAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	src := &biz.ChatMessage{
		ID: "old", SessionID: parent.ID, Role: "user", Content: "hi",
		Active: true, CreatedAt: srcAt, Metadata: map[string]any{"k": "v"},
	}
	got, err := msgRepo.InsertClone(ctx, child.ID, src)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == "old" || got.SessionID != child.ID {
		t.Fatalf("clone ids %+v", got)
	}
	if !got.CreatedAt.Equal(srcAt) {
		t.Fatalf("created_at %v want %v", got.CreatedAt, srcAt)
	}
	if got.Metadata["forked_from_message_id"] != "old" || got.Metadata["k"] != "v" {
		t.Fatalf("metadata %+v", got.Metadata)
	}
}
