package model

import "time"

// UserIdentity maps an external IdP subject to a Portal user.
type UserIdentity struct {
	Provider  string    `gorm:"column:provider;primaryKey;size:32"`
	Subject   string    `gorm:"column:subject;primaryKey;size:128"`
	UserID    string    `gorm:"column:user_id;size:36;not null;index"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (UserIdentity) TableName() string { return "user_identities" }
