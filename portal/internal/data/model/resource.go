package model

import "time"

// Resource 统一资源归属表（agent/tool/skill 元数据权威）
type Resource struct {
	ID           string    `gorm:"column:id;primaryKey;size:36"`
	Type         string    `gorm:"column:type;size:16;not null;uniqueIndex:idx_resource_type_payload"`
	Name         string    `gorm:"column:name;size:128;not null"`
	OwnerUserID  string    `gorm:"column:owner_user_id;size:36;not null;index"`
	Visibility   string    `gorm:"column:visibility;size:16;not null"`
	HomeOrgID    string    `gorm:"column:home_org_id;size:36;index"`
	BoundAgentID string    `gorm:"column:bound_agent_id;size:36;index"`
	PayloadRef   string    `gorm:"column:payload_ref;size:36;not null;uniqueIndex:idx_resource_type_payload"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null"`
}

func (Resource) TableName() string {
	return "resources"
}

// ResourceGrant 显式资源授权表
type ResourceGrant struct {
	ResourceID  string    `gorm:"column:resource_id;primaryKey;size:36"`
	GranteeType string    `gorm:"column:grantee_type;primaryKey;size:16"` // user|org
	GranteeID   string    `gorm:"column:grantee_id;primaryKey;size:36"`
	Perm        string    `gorm:"column:perm;size:16;not null"` // view|use|edit|admin
	CreatedAt   time.Time `gorm:"column:created_at;not null"`
}

func (ResourceGrant) TableName() string {
	return "resource_grants"
}
