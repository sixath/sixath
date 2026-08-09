package model

import "time"

// User 用户表
type User struct {
	ID              string     `gorm:"column:id;primaryKey;size:36"`
	Name            string     `gorm:"column:name;size:128;not null"`
	Email           *string    `gorm:"column:email;size:255;uniqueIndex"`
	PasswordHash    *string    `gorm:"column:password_hash;size:255"`
	EmailVerifiedAt *time.Time `gorm:"column:email_verified_at"`
	CreatedAt       time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;not null"`
}

func (User) TableName() string {
	return "users"
}

// Org 组织表
type Org struct {
	ID        string    `gorm:"column:id;primaryKey;size:36"`
	Name      string    `gorm:"column:name;size:128;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (Org) TableName() string {
	return "orgs"
}

// OrgMember 组织成员表
type OrgMember struct {
	OrgID     string    `gorm:"column:org_id;primaryKey;size:36"`
	UserID    string    `gorm:"column:user_id;primaryKey;size:36;index"`
	Role      string    `gorm:"column:role;size:16;not null"` // owner|member
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (OrgMember) TableName() string {
	return "org_members"
}

// UserToken Bearer token 哈希表（SHA-256 hex）
type UserToken struct {
	TokenHash string    `gorm:"column:token_hash;primaryKey;size:64"`
	UserID    string    `gorm:"column:user_id;size:36;not null;index"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (UserToken) TableName() string {
	return "user_tokens"
}

// OrgInvite 组织邀请表（token 仅存 SHA-256 hex 哈希）
type OrgInvite struct {
	ID         string     `gorm:"column:id;primaryKey;size:36"`
	OrgID      string     `gorm:"column:org_id;size:36;not null;index"`
	TokenHash  string     `gorm:"column:token_hash;size:64;not null;index"`
	CreatedBy  string     `gorm:"column:created_by;size:36;not null;index"`
	MaxUses    int        `gorm:"column:max_uses;not null;default:1"`   // 1=单次，0=无限，>1=上限
	UsedCount  int        `gorm:"column:used_count;not null;default:0"`
	ExpiresAt  *time.Time `gorm:"column:expires_at"`
	RevokedAt  *time.Time `gorm:"column:revoked_at"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null"`
}

func (OrgInvite) TableName() string {
	return "org_invites"
}

// EmailVerifyToken 邮箱验证 token（SMTP 开启时使用）
type EmailVerifyToken struct {
	TokenHash string    `gorm:"column:token_hash;primaryKey;size:64"`
	UserID    string    `gorm:"column:user_id;size:36;not null;index"`
	ExpiresAt time.Time `gorm:"column:expires_at;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (EmailVerifyToken) TableName() string {
	return "email_verify_tokens"
}
