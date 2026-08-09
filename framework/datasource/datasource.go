package datasource

import (
	"context"
	"encoding/json"
	"strings"
)

// DataSource 表示可连接、健康检查与关闭的数据源抽象。
// 供 list_tables / describe_table / 执行器按 ID 获取并使用。
type DataSource interface {
	ID() string
	// Type 返回数据源类型，如 TypeMySQL、TypeElasticsearch。
	Type() string
	Ping(ctx context.Context) error
	Close() error
}

// Config 为数据源连接配置。
// 敏感字段（如 Password）建议从环境变量或密钥服务读取，不落明文配置。
type Config struct {
	ID              string `json:"id" yaml:"id"`
	Type            string `json:"type" yaml:"type"` // 如 "mysql"
	DSN             string `json:"dsn" yaml:"dsn"`   // 完整 DSN，与 Host/Port/User/Password/DBName 二选一
	Host            string `json:"host" yaml:"host"`
	Port            int    `json:"port" yaml:"port"`
	User            string `json:"user" yaml:"user"`
	Password        string `json:"password" yaml:"password"`
	DBName          string `json:"dbname" yaml:"dbname"`
	MaxOpenConns    int    `json:"max_open_conns" yaml:"max_open_conns"`
	MaxIdleConns    int    `json:"max_idle_conns" yaml:"max_idle_conns"`
	ConnMaxLifetime int    `json:"conn_max_lifetime_sec" yaml:"conn_max_lifetime_sec"` // 秒
	ReadOnly        bool   `json:"read_only" yaml:"read_only"`
}

// intFromAny 解析 JSON/structpb.AsInterface 中可能出现的 port 等整数字段（float64、int、json.Number 等）。
func intFromAny(v interface{}) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case float32:
		return int(x), true
	case int:
		return x, true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case uint32:
		return int(x), true
	case uint64:
		return int(x), true
	case json.Number:
		i, err := x.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

// ConfigFromMap 从 map（如 portal 存储的 config.datasource）解析为 Config。
func ConfigFromMap(m map[string]interface{}) Config {
	var c Config
	if m == nil {
		return c
	}
	if v, ok := m["id"].(string); ok {
		c.ID = strings.TrimSpace(v)
	}
	if v, ok := m["type"].(string); ok {
		c.Type = strings.TrimSpace(strings.ToLower(v))
	}
	if v, ok := m["dsn"].(string); ok {
		c.DSN = v
	}
	if v, ok := m["host"].(string); ok {
		c.Host = v
	}
	if p, ok := intFromAny(m["port"]); ok {
		c.Port = p
	}
	if v, ok := m["user"].(string); ok {
		c.User = v
	}
	if v, ok := m["password"].(string); ok {
		c.Password = v
	}
	if v, ok := m["dbname"].(string); ok {
		c.DBName = v
	}
	if p, ok := intFromAny(m["max_open_conns"]); ok {
		c.MaxOpenConns = p
	}
	if p, ok := intFromAny(m["max_idle_conns"]); ok {
		c.MaxIdleConns = p
	}
	if p, ok := intFromAny(m["conn_max_lifetime_sec"]); ok {
		c.ConnMaxLifetime = p
	}
	if v, ok := m["read_only"].(bool); ok {
		c.ReadOnly = v
	}
	return c
}
