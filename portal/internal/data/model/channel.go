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
		str, ok := value.(string)
		if !ok {
			return errors.New("failed to unmarshal StringSlice")
		}
		bytes = []byte(str)
	}
	return json.Unmarshal(bytes, s)
}

// Channel 渠道表
type Channel struct {
	ID            string      `gorm:"column:id;primaryKey;size:36"`
	ChannelID     string      `gorm:"column:channel_id;uniqueIndex;size:64;not null"`
	Type          string      `gorm:"column:type;size:16;not null"` // web, api, webhook, wxpusher, wecom, wecom_bot
	DefaultAgent  string      `gorm:"column:default_agent;size:36"`
	AllowedAgents StringSlice `gorm:"column:allowed_agents;type:json"`
	Enabled       bool        `gorm:"column:enabled;not null;default:true"`
	WebhookPath   string      `gorm:"column:webhook_path;size:256"`
	WebhookSecret string      `gorm:"column:webhook_secret;size:256"`
	IPWhitelist   StringSlice `gorm:"column:ip_whitelist;type:json"`
	// WxPusher 渠道配置（type=wxpusher 时使用）
	AppToken    string      `gorm:"column:app_token;size:256"`
	DefaultUids StringSlice `gorm:"column:default_uids;type:json"`
	// WeCom 群机器人（type=wecom 时使用）
	WebhookURL string `gorm:"column:webhook_url;size:512"`
	// WeCom Bot（type=wecom_bot 时使用）；DB 列 bot_secret，API/JSON 对外仍用 secret
	BotID            string      `gorm:"column:bot_id;size:128"`
	BotSecret        string      `gorm:"column:bot_secret;size:256"`
	BotNames         StringSlice `gorm:"column:bot_names;type:json"`
	WSURL            string      `gorm:"column:ws_url;size:512"`
	CorpID           string      `gorm:"column:corp_id;size:64"`
	CorpSecret       string      `gorm:"column:corp_secret;size:256"`
	DefaultReplyMode string      `gorm:"column:default_reply_mode;size:16"`
	CreatedAt        time.Time   `gorm:"column:created_at;not null"`
	UpdatedAt        time.Time   `gorm:"column:updated_at;not null"`
}

func (Channel) TableName() string {
	return "channels"
}
