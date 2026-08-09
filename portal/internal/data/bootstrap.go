package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"backend/internal/conf"
	"backend/internal/data/model"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultBootstrapUserID = "bootstrap"
	defaultBootstrapOrgID  = "default"
	defaultBootstrapToken  = "dev-bootstrap-token"
)

// BootstrapACL creates the local ACL anchor identities, backfills legacy data,
// and creates private resources for agents and tools created before ACL support.
func BootstrapACL(ctx context.Context, db *gorm.DB, auth *conf.Auth, dataRoot string) error {
	if db == nil {
		return gorm.ErrInvalidDB
	}

	// dataRoot is intentionally accepted here so bootstrap configuration remains
	// coupled to the data lifecycle even though this migration does not write files.
	_ = dataRoot

	bootstrapUserID := defaultBootstrapUserID
	bootstrapOrgID := defaultBootstrapOrgID
	bootstrapToken := defaultBootstrapToken
	servicePrincipalUserID := bootstrapUserID
	if auth != nil {
		bootstrapUserID = defaultIfEmpty(auth.GetBootstrapUserId(), bootstrapUserID)
		bootstrapOrgID = defaultIfEmpty(auth.GetBootstrapOrgId(), bootstrapOrgID)
		bootstrapToken = defaultIfEmpty(auth.GetBootstrapToken(), bootstrapToken)
		servicePrincipalUserID = defaultIfEmpty(auth.GetServicePrincipalUserId(), bootstrapUserID)
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureUser(tx, bootstrapUserID); err != nil {
			return err
		}
		if err := ensureBootstrapEmailPassword(tx, auth, bootstrapUserID); err != nil {
			return err
		}
		if err := ensureUser(tx, servicePrincipalUserID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.Org{
			ID:   bootstrapOrgID,
			Name: bootstrapOrgID,
		}).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "org_id"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"role"}),
		}).Create(&model.OrgMember{
			OrgID:  bootstrapOrgID,
			UserID: bootstrapUserID,
			Role:   "owner",
		}).Error; err != nil {
			return err
		}

		sum := sha256.Sum256([]byte(bootstrapToken))
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "token_hash"}},
			DoUpdates: clause.AssignmentColumns([]string{"user_id"}),
		}).Create(&model.UserToken{
			TokenHash: hex.EncodeToString(sum[:]),
			UserID:    bootstrapUserID,
		}).Error; err != nil {
			return err
		}

		if err := bootstrapResources(tx, bootstrapUserID, bootstrapOrgID); err != nil {
			return err
		}
		return tx.Model(&model.ChatSession{}).
			Where("user_id = ? OR user_id IS NULL", "").
			Update("user_id", bootstrapUserID).Error
	})
}

func defaultIfEmpty(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

func ensureUser(db *gorm.DB, userID string) error {
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.User{
		ID:   userID,
		Name: userID,
	}).Error
}

func ensureBootstrapEmailPassword(db *gorm.DB, auth *conf.Auth, bootstrapUserID string) error {
	if auth == nil {
		return nil
	}
	email := strings.TrimSpace(auth.GetBootstrapEmail())
	password := auth.GetBootstrapPassword()
	if email == "" || password == "" {
		return nil
	}
	// bcrypt cost 12 matches internal/biz/password.go HashPassword.
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	passwordHash := string(hash)
	emailPtr := &email
	hashPtr := &passwordHash
	result := db.Model(&model.User{}).Where("id = ?", bootstrapUserID).Updates(map[string]any{
		"email":         emailPtr,
		"password_hash": hashPtr,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func bootstrapResources(db *gorm.DB, ownerUserID, homeOrgID string) error {
	type payload struct {
		ID   string
		Name string
	}

	var agents []payload
	if err := db.Model(&model.Agent{}).Select("id", "name").Find(&agents).Error; err != nil {
		return err
	}
	for _, agent := range agents {
		if err := createBootstrapResource(db, "agent", agent.ID, agent.Name, ownerUserID, homeOrgID); err != nil {
			return err
		}
	}

	var tools []payload
	if err := db.Model(&model.Tool{}).Select("id", "name").Find(&tools).Error; err != nil {
		return err
	}
	for _, tool := range tools {
		if err := createBootstrapResource(db, "tool", tool.ID, tool.Name, ownerUserID, homeOrgID); err != nil {
			return err
		}
	}
	return nil
}

func createBootstrapResource(db *gorm.DB, resourceType, payloadRef, name, ownerUserID, homeOrgID string) error {
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.Resource{
		ID:          uuid.NewString(),
		Type:        resourceType,
		Name:        name,
		OwnerUserID: ownerUserID,
		Visibility:  "private",
		HomeOrgID:   homeOrgID,
		PayloadRef:  payloadRef,
	}).Error
}
