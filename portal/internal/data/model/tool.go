package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// ToolConfig JSON 存储工具配置
type ToolConfig map[string]interface{}

func (c ToolConfig) Value() (driver.Value, error) {
	if c == nil {
		return "{}", nil
	}
	return json.Marshal(c)
}

func (c *ToolConfig) Scan(value interface{}) error {
	if value == nil {
		*c = make(map[string]interface{})
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to unmarshal ToolConfig")
	}
	return json.Unmarshal(bytes, c)
}

// Tool 工具表
type Tool struct {
	ID          string     `gorm:"column:id;primaryKey;size:36"`
	Name        string     `gorm:"column:name;uniqueIndex;size:128;not null"`
	Description string     `gorm:"column:description;type:text;not null"`
	Type        string     `gorm:"column:type;size:16;not null"`
	Config      ToolConfig `gorm:"column:config;type:json;not null"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;not null"`
}

func (Tool) TableName() string {
	return "tools"
}
