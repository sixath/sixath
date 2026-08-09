package data

import (
	"context"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ biz.IdentityRepo = (*identityRepo)(nil)

type identityRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewIdentityRepo creates a MySQL/GORM identity repository.
func NewIdentityRepo(data *Data, logger log.Logger) biz.IdentityRepo {
	if data == nil || data.db == nil {
		panic("NewIdentityRepo: Data.db is nil, database config required")
	}
	return &identityRepo{db: data.db, log: log.NewHelper(logger)}
}

func userModelToBiz(m *model.User) *biz.User {
	u := &biz.User{
		ID:              m.ID,
		Name:            m.Name,
		EmailVerifiedAt: m.EmailVerifiedAt,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
	if m.Email != nil {
		u.Email = *m.Email
	}
	if m.PasswordHash != nil {
		u.PasswordHash = *m.PasswordHash
	}
	return u
}

func orgModelToBiz(m *model.Org) *biz.Org {
	return &biz.Org{ID: m.ID, Name: m.Name, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

func (r *identityRepo) CreateUser(ctx context.Context, name string) (*biz.User, error) {
	m := &model.User{ID: uuid.NewString(), Name: name}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return nil, err
	}
	return userModelToBiz(m), nil
}

func (r *identityRepo) GetUser(ctx context.Context, id string) (*biz.User, error) {
	var m model.User
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return userModelToBiz(&m), nil
}

func (r *identityRepo) GetUserByEmail(ctx context.Context, email string) (*biz.User, error) {
	var m model.User
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return userModelToBiz(&m), nil
}

func (r *identityRepo) CreateUserWithPassword(ctx context.Context, id, name, email, passwordHash string) (*biz.User, error) {
	if id == "" {
		id = uuid.NewString()
	}
	emailPtr := &email
	hashPtr := &passwordHash
	m := &model.User{ID: id, Name: name, Email: emailPtr, PasswordHash: hashPtr}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return nil, err
	}
	return userModelToBiz(m), nil
}

func (r *identityRepo) SetEmailVerified(ctx context.Context, userID string, at time.Time) error {
	result := r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Update("email_verified_at", at)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *identityRepo) SetUserEmailPassword(ctx context.Context, userID, email, passwordHash string) error {
	emailPtr := &email
	hashPtr := &passwordHash
	result := r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
		"email":         emailPtr,
		"password_hash": hashPtr,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *identityRepo) CreateOrg(ctx context.Context, name string) (*biz.Org, error) {
	m := &model.Org{ID: uuid.NewString(), Name: name}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return nil, err
	}
	return orgModelToBiz(m), nil
}

func (r *identityRepo) GetOrg(ctx context.Context, orgID string) (*biz.Org, error) {
	var m model.Org
	if err := r.db.WithContext(ctx).Where("id = ?", orgID).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return orgModelToBiz(&m), nil
}

func (r *identityRepo) AddMember(ctx context.Context, orgID, userID, role string) error {
	m := &model.OrgMember{OrgID: orgID, UserID: userID, Role: role}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "org_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"role"}),
	}).Create(m).Error
}

func (r *identityRepo) MemberRole(ctx context.Context, orgID, userID string) (string, error) {
	var m model.OrgMember
	if err := r.db.WithContext(ctx).Where("org_id = ? AND user_id = ?", orgID, userID).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", nil
		}
		return "", err
	}
	return m.Role, nil
}

func (r *identityRepo) UserOrgIDs(ctx context.Context, userID string) ([]string, error) {
	var orgIDs []string
	err := r.db.WithContext(ctx).Model(&model.OrgMember{}).
		Where("user_id = ?", userID).
		Order("org_id ASC").
		Pluck("org_id", &orgIDs).Error
	return orgIDs, err
}

type orgMembershipRow struct {
	OrgID string
	Name  string
	Role  string
}

func (r *identityRepo) ListUserOrgs(ctx context.Context, userID string) ([]biz.OrgMembership, error) {
	var rows []orgMembershipRow
	err := r.db.WithContext(ctx).
		Table("org_members om").
		Select("om.org_id, o.name, om.role").
		Joins("JOIN orgs o ON o.id = om.org_id").
		Where("om.user_id = ?", userID).
		Order("o.name ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]biz.OrgMembership, len(rows))
	for i, row := range rows {
		out[i] = biz.OrgMembership{OrgID: row.OrgID, Name: row.Name, Role: row.Role}
	}
	return out, nil
}

func (r *identityRepo) UpsertTokenHash(ctx context.Context, userID, tokenHash string) error {
	m := &model.UserToken{TokenHash: tokenHash, UserID: userID}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "token_hash"}},
		DoUpdates: clause.AssignmentColumns([]string{"user_id"}),
	}).Create(m).Error
}

func (r *identityRepo) UserIDByTokenHash(ctx context.Context, tokenHash string) (string, error) {
	var m model.UserToken
	if err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", ErrNotFound
		}
		return "", err
	}
	return m.UserID, nil
}

func (r *identityRepo) CreateVerifyToken(ctx context.Context, userID string, expiresAt time.Time) (string, error) {
	plain, err := biz.GenerateOpaqueToken()
	if err != nil {
		return "", err
	}
	m := &model.EmailVerifyToken{
		TokenHash: biz.HashTokenSHA256Hex(plain),
		UserID:    userID,
		ExpiresAt: expiresAt,
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return "", err
	}
	return plain, nil
}

func (r *identityRepo) ConsumeVerifyToken(ctx context.Context, tokenHash string) (string, error) {
	var userID string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m model.EmailVerifyToken
		if err := tx.Where("token_hash = ?", tokenHash).First(&m).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrNotFound
			}
			return err
		}
		now := time.Now()
		if !m.ExpiresAt.After(now) {
			return ErrNotFound
		}
		userID = m.UserID
		result := tx.Where("token_hash = ? AND expires_at > ?", tokenHash, now).
			Delete(&model.EmailVerifyToken{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		return nil
	})
	return userID, err
}
