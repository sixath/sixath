package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// AgentAssetBinding is one Loadout explicit bind row (design §4.1).
type AgentAssetBinding struct {
	AgentID    string          `gorm:"column:agent_id;primaryKey;size:36"`
	AssetKind  string          `gorm:"column:asset_kind;primaryKey;size:64"`
	AssetID    string          `gorm:"column:asset_id;primaryKey;size:256"`
	Hub        string          `gorm:"column:hub;size:64;not null;default:local"`
	Name       string          `gorm:"column:name;size:256;not null;default:''"`
	Status     string          `gorm:"column:status;size:32;not null;default:active"`
	Priority   int             `gorm:"column:priority;not null;default:0"`
	OwnerID    string          `gorm:"column:owner_id;size:36"`
	Visibility string          `gorm:"column:visibility;size:32"`
	Metadata   BindingMetadata `gorm:"column:metadata;type:json"`
	CreatedAt  time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt  time.Time       `gorm:"column:updated_at;not null"`
}

func (AgentAssetBinding) TableName() string { return "agent_asset_bindings" }

// BindingMetadata is optional JSON blob on a binding row.
type BindingMetadata map[string]any

func (m BindingMetadata) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

func (m *BindingMetadata) Scan(value interface{}) error {
	if value == nil {
		*m = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		s, ok := value.(string)
		if !ok {
			return errors.New("failed to unmarshal BindingMetadata")
		}
		bytes = []byte(s)
	}
	return json.Unmarshal(bytes, m)
}
