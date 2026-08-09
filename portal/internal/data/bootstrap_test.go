package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"backend/internal/biz"
	"backend/internal/conf"
	"backend/internal/data/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBootstrapACLIsIdempotentAndBackfillsExistingRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:bootstrap-acl?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Org{}, &model.OrgMember{}, &model.UserToken{},
		&model.Agent{}, &model.Tool{}, &model.Resource{}, &model.ChatSession{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := db.Create(&model.Agent{ID: "agent-1", Name: "Agent One", Workspace: "workspace"}).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := db.Create(&model.Tool{ID: "tool-1", Name: "Tool One", Description: "tool", Type: "builtin"}).Error; err != nil {
		t.Fatalf("create tool: %v", err)
	}
	if err := db.Create(&model.ChatSession{ID: "session-1", AgentID: "agent-1", Title: "existing"}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	auth := &conf.Auth{
		BootstrapUserId:        "bootstrap-user",
		BootstrapOrgId:         "bootstrap-org",
		BootstrapToken:         "bootstrap-token",
		ServicePrincipalUserId: "service-principal",
	}
	for i := 0; i < 2; i++ {
		if err := BootstrapACL(context.Background(), db, auth, "./data"); err != nil {
			t.Fatalf("bootstrap run %d: %v", i+1, err)
		}
	}

	for _, userID := range []string{"bootstrap-user", "service-principal"} {
		var count int64
		if err := db.Model(&model.User{}).Where("id = ?", userID).Count(&count).Error; err != nil {
			t.Fatalf("count user %q: %v", userID, err)
		}
		if count != 1 {
			t.Fatalf("user %q count = %d, want 1", userID, count)
		}
	}

	var members int64
	if err := db.Model(&model.OrgMember{}).Where("org_id = ? AND user_id = ?", "bootstrap-org", "bootstrap-user").Count(&members).Error; err != nil {
		t.Fatalf("count membership: %v", err)
	}
	if members != 1 {
		t.Fatalf("membership count = %d, want 1", members)
	}

	sum := sha256.Sum256([]byte("bootstrap-token"))
	var token model.UserToken
	if err := db.First(&token, "token_hash = ?", hex.EncodeToString(sum[:])).Error; err != nil {
		t.Fatalf("load bootstrap token: %v", err)
	}
	if token.UserID != "bootstrap-user" {
		t.Fatalf("token user = %q, want bootstrap-user", token.UserID)
	}

	var resources []model.Resource
	if err := db.Order("type ASC").Find(&resources).Error; err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("resource count = %d, want 2", len(resources))
	}
	for _, resource := range resources {
		if resource.OwnerUserID != "bootstrap-user" || resource.HomeOrgID != "bootstrap-org" || resource.Visibility != "private" || resource.BoundAgentID != "" {
			t.Fatalf("unexpected resource ownership: %+v", resource)
		}
	}

	var session model.ChatSession
	if err := db.First(&session, "id = ?", "session-1").Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if session.UserID != "bootstrap-user" {
		t.Fatalf("session user = %q, want bootstrap-user", session.UserID)
	}
}

func TestBootstrapACLEmailPasswordIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:bootstrap-email?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Org{}, &model.OrgMember{}, &model.UserToken{},
		&model.Agent{}, &model.Tool{}, &model.Resource{}, &model.ChatSession{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	auth := &conf.Auth{
		BootstrapUserId:   "bootstrap-user",
		BootstrapEmail:    "admin@example.com",
		BootstrapPassword: "bootstrap-secret",
	}
	for i := 0; i < 2; i++ {
		if err := BootstrapACL(context.Background(), db, auth, "./data"); err != nil {
			t.Fatalf("bootstrap run %d: %v", i+1, err)
		}
	}

	var user model.User
	if err := db.First(&user, "id = ?", "bootstrap-user").Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if user.Email == nil || *user.Email != "admin@example.com" {
		t.Fatalf("email = %v, want admin@example.com", user.Email)
	}
	if user.PasswordHash == nil || !biz.CheckPassword(*user.PasswordHash, "bootstrap-secret") {
		t.Fatalf("password hash not set or wrong")
	}
}
