package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// ModelConfig JSON 存储模型配置
type ModelConfig map[string]interface{}

func (c ModelConfig) Value() (driver.Value, error) {
	if c == nil {
		return "{}", nil
	}
	return json.Marshal(c)
}

func (c *ModelConfig) Scan(value interface{}) error {
	if value == nil {
		*c = make(map[string]interface{})
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to unmarshal ModelConfig")
	}
	return json.Unmarshal(bytes, c)
}

// Agent Agent 表
type Agent struct {
	ID             string             `gorm:"column:id;primaryKey;size:36"`
	Name           string             `gorm:"column:name;uniqueIndex;size:128;not null"`
	Description    string             `gorm:"column:description;type:text"`
	SystemPrompt   string             `gorm:"column:system_prompt;type:text"`
	ModelConfig    ModelConfig        `gorm:"column:model_config;type:json;not null"`
	Workspace      string             `gorm:"column:workspace;size:512;not null"`
	DebugRun       bool               `gorm:"column:debug_run;not null;default:false"`
	WecomChannelID string             `gorm:"column:wecom_channel_id;size:36"`
	RuntimeTools   RuntimeToolsConfig `gorm:"column:runtime_tools;type:json"`
	CreatedAt      time.Time          `gorm:"column:created_at;not null"`
	UpdatedAt      time.Time          `gorm:"column:updated_at;not null"`
}

func (Agent) TableName() string {
	return "agents"
}
