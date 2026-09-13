package data

import (
	"context"
	"fmt"
	"testing"
	"time"

	"backend/internal/biz"
)

// 需要 CGO（go-sqlite3）；本地 CGO_ENABLED=0 时会失败，CI 上运行。
func TestListBySessionBefore_PaginatesWithoutGapsOrDuplicates(t *testing.T) {
	db := openChatRewindTestDB(t)
	sessRepo := &chatSessionRepo{db: db}
	msgRepo := &chatMessageRepo{db: db}
	ctx := context.Background()

	sess, err := sessRepo.Create(ctx, "u1", "a1", "page-test", "")
	if err != nil {
		t.Fatal(err)
	}

	// 关键场景：同一时间戳内多条消息（批量写入 / 同秒），仅按 created_at 分页会丢或重。
	base := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	const total = 25
	for i := 0; i < total; i++ {
		created := base.Add(time.Duration(i/5) * time.Second)
		if _, err := msgRepo.Create(ctx, sess.ID, "user", fmt.Sprintf("m%02d", i), nil); err != nil {
			t.Fatal(err)
		}
		// Create 使用数据库默认时间；这里显式回写以制造同秒批次。
		if err := db.Table("chat_messages").
			Where("session_id = ? AND content = ?", sess.ID, fmt.Sprintf("m%02d", i)).
			Update("created_at", created).Error; err != nil {
			t.Fatal(err)
		}
	}

	seen := map[string]int{}
	cursor := biz.MessageCursor{}
	pages := 0
	for {
		page, next, err := msgRepo.ListBySessionBefore(ctx, sess.ID, cursor, 10)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		if len(page) == 0 {
			break
		}
		// 升序输出（与 ListBySession 一致）
		for i := 1; i < len(page); i++ {
			if page[i].CreatedAt.Before(page[i-1].CreatedAt) {
				t.Fatalf("page %d not ascending: %s before %s", pages, page[i].CreatedAt, page[i-1].CreatedAt)
			}
		}
		for _, m := range page {
			seen[m.Content]++
		}
		pages++
		if next == "" {
			break
		}
		decoded, err := biz.DecodeMessageCursor(next)
		if err != nil {
			t.Fatalf("next cursor %q not decodable: %v", next, err)
		}
		cursor = decoded
		if pages > total {
			t.Fatal("pagination did not terminate")
		}
	}

	if len(seen) != total {
		t.Fatalf("collected %d distinct messages, want %d", len(seen), total)
	}
	for content, n := range seen {
		if n != 1 {
			t.Fatalf("message %s returned %d times (duplicate across pages)", content, n)
		}
	}
}

func TestListBySessionBefore_FirstPageIsNewest(t *testing.T) {
	db := openChatRewindTestDB(t)
	sessRepo := &chatSessionRepo{db: db}
	msgRepo := &chatMessageRepo{db: db}
	ctx := context.Background()

	sess, err := sessRepo.Create(ctx, "u1", "a1", "page-test-2", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := msgRepo.Create(ctx, sess.ID, "user", fmt.Sprintf("n%d", i), nil); err != nil {
			t.Fatal(err)
		}
		if err := db.Table("chat_messages").
			Where("session_id = ? AND content = ?", sess.ID, fmt.Sprintf("n%d", i)).
			Update("created_at", time.Date(2026, 9, 12, 11, 0, i, 0, time.UTC)).Error; err != nil {
			t.Fatal(err)
		}
	}

	page, next, err := msgRepo.ListBySessionBefore(ctx, sess.ID, biz.MessageCursor{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 {
		t.Fatalf("len=%d want 2", len(page))
	}
	// 最新一页取最新的两条，且升序返回 → n3, n4
	if page[0].Content != "n3" || page[1].Content != "n4" {
		t.Fatalf("first page=%s,%s want n3,n4", page[0].Content, page[1].Content)
	}
	if next == "" {
		t.Fatal("expected next cursor when more messages exist")
	}

	// 再往前一页：n1, n2
	cursor, err := biz.DecodeMessageCursor(next)
	if err != nil {
		t.Fatal(err)
	}
	page2, next2, err := msgRepo.ListBySessionBefore(ctx, sess.ID, cursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 || page2[0].Content != "n1" || page2[1].Content != "n2" {
		t.Fatalf("second page=%+v want n1,n2", contentList(page2))
	}
	if next2 == "" {
		t.Fatal("expected next cursor for the earliest page still containing data")
	}

	// 最后一页：n0，且没有更早的游标
	cursor2, err := biz.DecodeMessageCursor(next2)
	if err != nil {
		t.Fatal(err)
	}
	page3, next3, err := msgRepo.ListBySessionBefore(ctx, sess.ID, cursor2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page3) != 1 || page3[0].Content != "n0" {
		t.Fatalf("third page=%+v want n0", contentList(page3))
	}
	if next3 != "" {
		t.Fatalf("earliest page must not yield a cursor, got %q", next3)
	}
}

func TestListBySessionBefore_ExcludesInactiveMessages(t *testing.T) {
	db := openChatRewindTestDB(t)
	sessRepo := &chatSessionRepo{db: db}
	msgRepo := &chatMessageRepo{db: db}
	ctx := context.Background()

	sess, err := sessRepo.Create(ctx, "u1", "a1", "page-test-3", "")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := 0; i < 3; i++ {
		m, err := msgRepo.Create(ctx, sess.ID, "user", fmt.Sprintf("r%d", i), nil)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
		if err := db.Table("chat_messages").
			Where("id = ?", m.ID).
			Update("created_at", time.Date(2026, 9, 12, 12, 0, i, 0, time.UTC)).Error; err != nil {
			t.Fatal(err)
		}
	}
	// rewind 到第 2 条：第 2、3 条变为 inactive
	if _, err := msgRepo.SoftDeactivateAfter(ctx, sess.ID, time.Date(2026, 9, 12, 12, 0, 1, 0, time.UTC), ids[1]); err != nil {
		t.Fatal(err)
	}

	page, next, err := msgRepo.ListBySessionBefore(ctx, sess.ID, biz.MessageCursor{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].Content != "r0" {
		t.Fatalf("page=%+v want only r0 (inactive hidden)", contentList(page))
	}
	if next != "" {
		t.Fatalf("no earlier messages expected, got cursor %q", next)
	}
}

func contentList(msgs []*biz.ChatMessage) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Content)
	}
	return out
}
