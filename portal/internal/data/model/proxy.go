package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// ProxyNoProxy is a JSON string list stored in proxies.no_proxy.
type ProxyNoProxy []string

func (a ProxyNoProxy) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	return json.Marshal(a)
}

func (a *ProxyNoProxy) Scan(value interface{}) error {
	if value == nil {
		*a = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		s, ok := value.(string)
		if !ok {
			return errors.New("failed to unmarshal ProxyNoProxy")
		}
		bytes = []byte(s)
	}
	return json.Unmarshal(bytes, a)
}

// Proxy is a named HTTP or SOCKS5 egress tunnel.
// id is a user slug: ^[a-z][a-z0-9_-]{0,35}$
type Proxy struct {
	ID          string       `gorm:"column:id;primaryKey;size:36"`
	Name        string       `gorm:"column:name;size:128;not null;index:idx_proxies_name"`
	Description string       `gorm:"column:description;type:text;not null"`
	Type        string       `gorm:"column:type;size:16;not null"`
	Host        string       `gorm:"column:host;size:256;not null"`
	Port        int          `gorm:"column:port;not null"`
	User        string       `gorm:"column:user;size:128;not null;default:''"`
	Password    string       `gorm:"column:password;size:256;not null;default:''"`
	NoProxy     ProxyNoProxy `gorm:"column:no_proxy;type:json"`
	CreatedAt   time.Time    `gorm:"column:created_at;not null"`
	UpdatedAt   time.Time    `gorm:"column:updated_at;not null"`
}

func (Proxy) TableName() string {
	return "proxies"
}
