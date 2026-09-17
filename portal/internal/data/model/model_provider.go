package model

import "time"

const (
	KindOpenAICompat = "openai_compat"
	KindDashScope    = "dashscope"
	SourceSync       = "sync"
	SourceManual     = "manual"
)

type ModelProvider struct {
	ID        string `gorm:"column:id;primaryKey;size:36"`
	Name      string `gorm:"column:name;size:128;not null"`
	Kind      string `gorm:"column:kind;size:32;not null"`
	BaseURL   string `gorm:"column:base_url;size:512"`
	APIKey    string `gorm:"column:api_key;size:512"`
	Enabled   bool   `gorm:"column:enabled;not null;default:1"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (ModelProvider) TableName() string { return "model_providers" }

type ModelCatalogEntry struct {
	ID                    string `gorm:"column:id;primaryKey;size:36"`
	ProviderID            string `gorm:"column:provider_id;size:36;uniqueIndex:uk_prov_model;index"`
	Model                 string `gorm:"column:model;size:256;uniqueIndex:uk_prov_model"`
	DisplayName           string `gorm:"column:display_name;size:256"`
	Hidden                bool   `gorm:"column:hidden;not null;default:0"`
	Source                string `gorm:"column:source;size:16;not null"`
	DisplayNameOverridden bool   `gorm:"column:display_name_overridden;not null;default:0"`
	HiddenOverridden      bool   `gorm:"column:hidden_overridden;not null;default:0"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (ModelCatalogEntry) TableName() string { return "model_catalog" }
