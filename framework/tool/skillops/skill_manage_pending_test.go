package toolskill

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestInMemorySkillManagePendingStore_SaveGetDelete(t *testing.T) {
	store := NewInMemorySkillManagePendingStore()
	ctx := context.Background()
	p := PendingSkillManage{
		Token:     "tok1",
		Action:    "create",
		Name:      "my-skill",
		Content:   "# body",
		CreatedAt: time.Now(),
	}
	if err := store.SavePending(ctx, "sess", p); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetPending(ctx, "sess", "tok1")
	if err != nil || got == nil || got.Name != "my-skill" {
		t.Fatalf("get: err=%v got=%#v", err, got)
	}
	if err := store.DeletePending(ctx, "sess", "tok1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.GetPending(ctx, "sess", "tok1"); got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestInMemorySkillManagePendingStore_SupersedesSameNameAction(t *testing.T) {
	store := NewInMemorySkillManagePendingStore()
	ctx := context.Background()
	_ = store.SavePending(ctx, "sess", PendingSkillManage{
		Token: "old", Action: "create", Name: "s1", CreatedAt: time.Now(),
	})
	_ = store.SavePending(ctx, "sess", PendingSkillManage{
		Token: "new", Action: "create", Name: "s1", CreatedAt: time.Now(),
	})
	if got, _ := store.GetPending(ctx, "sess", "old"); got != nil {
		t.Fatal("old token should be superseded")
	}
	if got, _ := store.GetPending(ctx, "sess", "new"); got == nil {
		t.Fatal("new token should remain")
	}
}

func TestSkillManagePreview_TruncatesCreate(t *testing.T) {
	long := strings.Repeat("a", 600)
	preview := skillManagePreview("create", "x", long, PendingSkillManage{})
	if len(preview) != skillManagePreviewMaxLen+3 {
		t.Fatalf("len=%d want %d", len(preview), skillManagePreviewMaxLen+3)
	}
	if !strings.HasSuffix(preview, "...") {
		t.Fatalf("expected suffix ... got %q", preview)
	}
}

func TestSkillManagePreview_Delete(t *testing.T) {
	preview := skillManagePreview("delete", "gone-skill", "", PendingSkillManage{})
	if preview != "Delete skill: gone-skill" {
		t.Fatalf("got %q", preview)
	}
}

func TestInMemorySkillManagePendingStore_TombstoneSuperseded(t *testing.T) {
	store := NewInMemorySkillManagePendingStore()
	ctx := context.Background()
	_ = store.SavePending(ctx, "sess", PendingSkillManage{
		Token: "old", Action: "patch", Name: "s1", CreatedAt: time.Now(),
	})
	_ = store.SavePending(ctx, "sess", PendingSkillManage{
		Token: "new", Action: "patch", Name: "s1", CreatedAt: time.Now(),
	})
	reason, ok := store.TombstoneReason(ctx, "sess", "old")
	if !ok || reason != "superseded" {
		t.Fatalf("tombstone: ok=%v reason=%q", ok, reason)
	}
}

func TestInMemorySkillManagePendingStore_TombstoneAlreadyUsed(t *testing.T) {
	store := NewInMemorySkillManagePendingStore()
	ctx := context.Background()
	_ = store.SavePending(ctx, "sess", PendingSkillManage{
		Token: "t1", Action: "patch", Name: "s1", CreatedAt: time.Now(),
	})
	_ = store.ConsumePending(ctx, "sess", "t1") // 仅成功消费写 already_used
	reason, ok := store.TombstoneReason(ctx, "sess", "t1")
	if !ok || reason != "already_used" {
		t.Fatalf("tombstone: ok=%v reason=%q", ok, reason)
	}
}

func TestInMemorySkillManagePendingStore_ExpireDeleteNoAlreadyUsed(t *testing.T) {
	store := NewInMemorySkillManagePendingStore()
	ctx := context.Background()
	_ = store.SavePending(ctx, "sess", PendingSkillManage{
		Token: "t1", Action: "patch", Name: "s1", CreatedAt: time.Now(),
	})
	_ = store.DeletePending(ctx, "sess", "t1") // 过期清理不得写 already_used
	if _, ok := store.TombstoneReason(ctx, "sess", "t1"); ok {
		t.Fatal("plain DeletePending must not tombstone as already_used")
	}
}
