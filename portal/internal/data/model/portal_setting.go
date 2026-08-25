package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// PortalSetting is a process-wide key/value JSON blob (UI 可改的全局配置).
type PortalSetting struct {
	Key       string    `gorm:"column:setting_key;primaryKey;size:64"`
	Value     string    `gorm:"column:setting_value;type:text"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (PortalSetting) TableName() string {
	return "portal_settings"
}

// PortalSettingJSON is JSON stored in setting_value.
type PortalSettingJSON map[string]any

func (c PortalSettingJSON) Value() (driver.Value, error) {
	if c == nil {
		return "{}", nil
	}
	return json.Marshal(c)
}

func (c *PortalSettingJSON) Scan(value interface{}) error {
	if value == nil {
		*c = make(map[string]any)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		s, ok := value.(string)
		if !ok {
			return errors.New("failed to unmarshal portal setting")
		}
		bytes = []byte(s)
	}
	return json.Unmarshal(bytes, c)
}
