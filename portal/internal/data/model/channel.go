package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// StringSlice JSON 存储字符串数组
type StringSlice []string

func (s StringSlice) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	return json.Marshal(s)
}

func (s *StringSlice) Scan(value interface{}) error {
	if value == nil {
		*s = []string{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to unmarshal StringSlice")
	}
	return json.Unmarshal(bytes, s)
}

// Channel 渠道表
type Channel struct {
	ID            string      `gorm:"column:id;primaryKey;size:36"`
	ChannelID     string      `gorm:"column:channel_id;uniqueIndex;size:64;not null"`
	Type          string      `gorm:"column:type;size:16;not null"` // web, api, webhook, wxpusher, wecom
	DefaultAgent  string      `gorm:"column:default_agent;size:36"`
	AllowedAgents StringSlice `gorm:"column:allowed_agents;type:json"`
	AutoRouteEnabled    bool `gorm:"column:auto_route_enabled;not null;default:1"`
	AutoRouteMention    bool `gorm:"column:auto_route_mention;not null;default:1"`
	AutoRouteClassifier bool `gorm:"column:auto_route_classifier;not null;default:1"`
	Enabled       bool        `gorm:"column:enabled;not null;default:true"`
	WebhookPath   string      `gorm:"column:webhook_path;size:256"`
	WebhookSecret string      `gorm:"column:webhook_secret;size:256"`
	IPWhitelist   StringSlice `gorm:"column:ip_whitelist;type:json"`
	// WxPusher 渠道配置（type=wxpusher 时使用）
	AppToken    string      `gorm:"column:app_token;size:256"`
	DefaultUids StringSlice `gorm:"column:default_uids;type:json"`
	// WeCom 群机器人（type=wecom 时使用）
	WebhookURL string `gorm:"column:webhook_url;size:512"`
	CreatedAt   time.Time   `gorm:"column:created_at;not null"`
	UpdatedAt   time.Time   `gorm:"column:updated_at;not null"`
}

func (Channel) TableName() string {
	return "channels"
}
