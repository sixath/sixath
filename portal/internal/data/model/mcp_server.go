package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// McpServerArgs JSON array of command-line args (nullable column args_json).
type McpServerArgs []string

func (a McpServerArgs) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	return json.Marshal(a)
}

func (a *McpServerArgs) Scan(value interface{}) error {
	if value == nil {
		*a = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		s, ok := value.(string)
		if !ok {
			return errors.New("failed to unmarshal McpServerArgs")
		}
		bytes = []byte(s)
	}
	return json.Unmarshal(bytes, a)
}

// McpServerEnv JSON object of environment variables (nullable column env_json).
type McpServerEnv map[string]string

func (e McpServerEnv) Value() (driver.Value, error) {
	if e == nil {
		return nil, nil
	}
	return json.Marshal(e)
}

func (e *McpServerEnv) Scan(value interface{}) error {
	if value == nil {
		*e = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		s, ok := value.(string)
		if !ok {
			return errors.New("failed to unmarshal McpServerEnv")
		}
		bytes = []byte(s)
	}
	return json.Unmarshal(bytes, e)
}

// McpServer MCP Server 配置表。
// id 为用户 slug：^[a-z][a-z0-9_-]{0,35}$（最多 36 字符，适配 resources.payload_ref）。
type McpServer struct {
	ID          string        `gorm:"column:id;primaryKey;size:36"`
	Name        string        `gorm:"column:name;size:128;not null"`
	Description string        `gorm:"column:description;type:text;not null"`
	Transport   string        `gorm:"column:transport;size:16;not null"`
	Endpoint    string        `gorm:"column:endpoint;size:512;not null;default:''"`
	Backend     string        `gorm:"column:backend;size:32;not null;default:''"`
	Command     string        `gorm:"column:command;size:256;not null;default:''"`
	ArgsJSON    McpServerArgs `gorm:"column:args_json;type:json"`
	EnvJSON     McpServerEnv  `gorm:"column:env_json;type:json"`
	TimeoutSec  int           `gorm:"column:timeout_sec;not null;default:60"`
	CreatedAt   time.Time     `gorm:"column:created_at;not null"`
	UpdatedAt   time.Time     `gorm:"column:updated_at;not null"`
}

func (McpServer) TableName() string {
	return "mcp_servers"
}
