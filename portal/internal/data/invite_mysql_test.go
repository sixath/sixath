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

func openInviteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.OrgInvite{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newInviteRepoForTest(db *gorm.DB) *inviteRepo {
	return &inviteRepo{db: db}
}

func TestIncrementInviteUsedSingleUse(t *testing.T) {
	db := openInviteTestDB(t)
	repo := newInviteRepoForTest(db)
	ctx := context.Background()

	invite, _, err := repo.CreateInvite(ctx, "org-1", "owner-1", 1, nil)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if err := repo.IncrementInviteUsed(ctx, invite.ID); err != nil {
		t.Fatalf("first IncrementInviteUsed: %v", err)
	}
	if err := repo.IncrementInviteUsed(ctx, invite.ID); err != ErrConflict {
		t.Fatalf("second IncrementInviteUsed = %v, want ErrConflict", err)
	}
}

func TestIncrementInviteUsedUnlimited(t *testing.T) {
	db := openInviteTestDB(t)
	repo := newInviteRepoForTest(db)
	ctx := context.Background()

	invite, _, err := repo.CreateInvite(ctx, "org-1", "owner-1", 0, nil)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	var stored model.OrgInvite
	if err := db.First(&stored, "id = ?", invite.ID).Error; err != nil {
		t.Fatalf("load invite: %v", err)
	}
	if stored.MaxUses != 0 {
		t.Fatalf("stored max_uses = %d, want 0", stored.MaxUses)
	}
	for i := 0; i < 3; i++ {
		if err := repo.IncrementInviteUsed(ctx, invite.ID); err != nil {
			t.Fatalf("IncrementInviteUsed #%d: %v", i+1, err)
		}
	}
}

func TestIncrementInviteUsedRejectsRevokedAndExpired(t *testing.T) {
	db := openInviteTestDB(t)
	repo := newInviteRepoForTest(db)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	expired, _, err := repo.CreateInvite(ctx, "org-1", "owner-1", 0, &past)
	if err != nil {
		t.Fatalf("CreateInvite expired: %v", err)
	}
	if err := repo.IncrementInviteUsed(ctx, expired.ID); err != ErrConflict {
		t.Fatalf("expired IncrementInviteUsed = %v, want ErrConflict", err)
	}

	active, _, err := repo.CreateInvite(ctx, "org-1", "owner-1", 0, nil)
	if err != nil {
		t.Fatalf("CreateInvite active: %v", err)
	}
	if err := repo.RevokeInvite(ctx, active.ID); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}
	if err := repo.IncrementInviteUsed(ctx, active.ID); err != ErrConflict {
		t.Fatalf("revoked IncrementInviteUsed = %v, want ErrConflict", err)
	}
}

func TestListInvitesByOrgOmitsPlainToken(t *testing.T) {
	db := openInviteTestDB(t)
	repo := newInviteRepoForTest(db)
	ctx := context.Background()

	created, plain, err := repo.CreateInvite(ctx, "org-1", "owner-1", 1, nil)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if plain == "" {
		t.Fatal("expected non-empty plain token from CreateInvite")
	}

	invites, err := repo.ListInvitesByOrg(ctx, "org-1")
	if err != nil {
		t.Fatalf("ListInvitesByOrg: %v", err)
	}
	if len(invites) != 1 {
		t.Fatalf("invite count = %d, want 1", len(invites))
	}
	if invites[0].ID != created.ID {
		t.Fatalf("listed invite id = %q, want %q", invites[0].ID, created.ID)
	}

	byHash, err := repo.GetInviteByTokenHash(ctx, biz.HashTokenSHA256Hex(plain))
	if err != nil {
		t.Fatalf("GetInviteByTokenHash: %v", err)
	}
	if byHash.ID != created.ID {
		t.Fatalf("lookup by hash id = %q, want %q", byHash.ID, created.ID)
	}
}
