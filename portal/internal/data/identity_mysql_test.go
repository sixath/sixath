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

func openIdentityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.EmailVerifyToken{}, &model.UserIdentity{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newIdentityRepoForTest(db *gorm.DB) *identityRepo {
	return &identityRepo{db: db}
}

func TestGetUserByEmailHitAndMiss(t *testing.T) {
	db := openIdentityTestDB(t)
	repo := newIdentityRepoForTest(db)
	ctx := context.Background()

	created, err := repo.CreateUserWithPassword(ctx, "user-1", "Alice", "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateUserWithPassword: %v", err)
	}

	got, err := repo.GetUserByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail hit: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("user id = %q, want %q", got.ID, created.ID)
	}

	if _, err := repo.GetUserByEmail(ctx, "missing@example.com"); err != ErrNotFound {
		t.Fatalf("GetUserByEmail miss = %v, want ErrNotFound", err)
	}
}

func TestCreateUserWithPasswordFieldMapping(t *testing.T) {
	db := openIdentityTestDB(t)
	repo := newIdentityRepoForTest(db)
	ctx := context.Background()

	created, err := repo.CreateUserWithPassword(ctx, "user-2", "Bob", "bob@example.com", "secret-hash")
	if err != nil {
		t.Fatalf("CreateUserWithPassword: %v", err)
	}
	if created.ID != "user-2" {
		t.Fatalf("id = %q, want user-2", created.ID)
	}
	if created.Name != "Bob" {
		t.Fatalf("name = %q, want Bob", created.Name)
	}
	if created.Email != "bob@example.com" {
		t.Fatalf("email = %q, want bob@example.com", created.Email)
	}
	if created.PasswordHash != "secret-hash" {
		t.Fatalf("password_hash = %q, want secret-hash", created.PasswordHash)
	}

	got, err := repo.GetUser(ctx, "user-2")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Email != created.Email || got.PasswordHash != created.PasswordHash {
		t.Fatalf("GetUser = %+v, want email/password from create", got)
	}
}

func TestCreateAndConsumeVerifyTokenSuccess(t *testing.T) {
	db := openIdentityTestDB(t)
	repo := newIdentityRepoForTest(db)
	ctx := context.Background()

	if _, err := repo.CreateUserWithPassword(ctx, "user-3", "Carol", "carol@example.com", "hash"); err != nil {
		t.Fatalf("CreateUserWithPassword: %v", err)
	}

	expires := time.Now().Add(time.Hour)
	plain, err := repo.CreateVerifyToken(ctx, "user-3", expires)
	if err != nil {
		t.Fatalf("CreateVerifyToken: %v", err)
	}
	if plain == "" {
		t.Fatal("expected non-empty plain verify token")
	}

	userID, err := repo.ConsumeVerifyToken(ctx, biz.HashTokenSHA256Hex(plain))
	if err != nil {
		t.Fatalf("ConsumeVerifyToken: %v", err)
	}
	if userID != "user-3" {
		t.Fatalf("userID = %q, want user-3", userID)
	}
}

func TestConsumeVerifyTokenExpired(t *testing.T) {
	db := openIdentityTestDB(t)
	repo := newIdentityRepoForTest(db)
	ctx := context.Background()

	if _, err := repo.CreateUserWithPassword(ctx, "user-4", "Dave", "dave@example.com", "hash"); err != nil {
		t.Fatalf("CreateUserWithPassword: %v", err)
	}

	expires := time.Now().Add(-time.Minute)
	plain, err := repo.CreateVerifyToken(ctx, "user-4", expires)
	if err != nil {
		t.Fatalf("CreateVerifyToken: %v", err)
	}

	if _, err := repo.ConsumeVerifyToken(ctx, biz.HashTokenSHA256Hex(plain)); err != ErrNotFound {
		t.Fatalf("ConsumeVerifyToken expired = %v, want ErrNotFound", err)
	}
}

func TestCreateUserForIdentity(t *testing.T) {
	db := openIdentityTestDB(t)
	repo := newIdentityRepoForTest(db)
	ctx := context.Background()
	verifiedAt := time.Now().Add(-time.Minute)

	user, err := repo.CreateUserForIdentity(ctx, "WeCom User", verifiedAt)
	if err != nil {
		t.Fatalf("CreateUserForIdentity: %v", err)
	}
	if user.Email != "" {
		t.Fatalf("email = %q, want empty", user.Email)
	}
	if user.PasswordHash != "" {
		t.Fatalf("password_hash = %q, want empty", user.PasswordHash)
	}
	if user.EmailVerifiedAt == nil || !user.EmailVerifiedAt.Equal(verifiedAt) {
		t.Fatalf("email_verified_at = %v, want %v", user.EmailVerifiedAt, verifiedAt)
	}

	got, err := repo.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Email != "" || got.PasswordHash != "" {
		t.Fatalf("GetUser = %+v, want empty email/password", got)
	}
}

func TestCreateIdentityDuplicateConflict(t *testing.T) {
	db := openIdentityTestDB(t)
	repo := newIdentityRepoForTest(db)
	ctx := context.Background()

	if _, err := repo.CreateUserWithPassword(ctx, "user-wecom", "Ada", "ada@example.com", "hash"); err != nil {
		t.Fatalf("CreateUserWithPassword: %v", err)
	}
	if err := repo.CreateIdentity(ctx, "wecom", "subject-1", "user-wecom"); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	if err := repo.CreateIdentity(ctx, "wecom", "subject-1", "user-wecom"); err != ErrConflict {
		t.Fatalf("duplicate CreateIdentity = %v, want ErrConflict", err)
	}
}

func TestGetIdentityHitAndMiss(t *testing.T) {
	db := openIdentityTestDB(t)
	repo := newIdentityRepoForTest(db)
	ctx := context.Background()

	if _, err := repo.CreateUserWithPassword(ctx, "user-wecom-2", "Bob", "bob@example.com", "hash"); err != nil {
		t.Fatalf("CreateUserWithPassword: %v", err)
	}
	if err := repo.CreateIdentity(ctx, "wecom", "subject-2", "user-wecom-2"); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}

	userID, err := repo.GetIdentity(ctx, "wecom", "subject-2")
	if err != nil {
		t.Fatalf("GetIdentity hit: %v", err)
	}
	if userID != "user-wecom-2" {
		t.Fatalf("userID = %q, want user-wecom-2", userID)
	}

	if _, err := repo.GetIdentity(ctx, "wecom", "missing"); err != ErrNotFound {
		t.Fatalf("GetIdentity miss = %v, want ErrNotFound", err)
	}
}

func TestListUsersQueryAndLimit(t *testing.T) {
	db := openIdentityTestDB(t)
	repo := newIdentityRepoForTest(db)
	ctx := context.Background()

	if _, err := repo.CreateUserWithPassword(ctx, "u1", "Alice", "alice@example.com", "hash"); err != nil {
		t.Fatalf("CreateUserWithPassword alice: %v", err)
	}
	if _, err := repo.CreateUserWithPassword(ctx, "u2", "Bob", "bob@example.com", "hash"); err != nil {
		t.Fatalf("CreateUserWithPassword bob: %v", err)
	}

	all, err := repo.ListUsers(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListUsers all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListUsers all count = %d, want 2", len(all))
	}

	filtered, err := repo.ListUsers(ctx, "ali", 10)
	if err != nil {
		t.Fatalf("ListUsers filtered: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != "u1" {
		t.Fatalf("ListUsers filtered = %#v, want Alice", filtered)
	}
}

func TestConsumeVerifyTokenTwice(t *testing.T) {
	db := openIdentityTestDB(t)
	repo := newIdentityRepoForTest(db)
	ctx := context.Background()

	if _, err := repo.CreateUserWithPassword(ctx, "user-5", "Eve", "eve@example.com", "hash"); err != nil {
		t.Fatalf("CreateUserWithPassword: %v", err)
	}

	expires := time.Now().Add(time.Hour)
	plain, err := repo.CreateVerifyToken(ctx, "user-5", expires)
	if err != nil {
		t.Fatalf("CreateVerifyToken: %v", err)
	}
	hash := biz.HashTokenSHA256Hex(plain)

	if _, err := repo.ConsumeVerifyToken(ctx, hash); err != nil {
		t.Fatalf("first ConsumeVerifyToken: %v", err)
	}
	if _, err := repo.ConsumeVerifyToken(ctx, hash); err != ErrNotFound {
		t.Fatalf("second ConsumeVerifyToken = %v, want ErrNotFound", err)
	}
}
