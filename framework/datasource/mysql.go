package datasource

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql"
)

// mysqlDataSource 实现 DataSource，并暴露底层 *sql.DB 供执行器与元数据使用。
type mysqlDataSource struct {
	id string
	db *sql.DB
}

func (m *mysqlDataSource) ID() string   { return m.id }
func (m *mysqlDataSource) Type() string { return TypeMySQL }

func (m *mysqlDataSource) Ping(ctx context.Context) error {
	return m.db.PingContext(ctx)
}

func (m *mysqlDataSource) Close() error {
	return m.db.Close()
}

// DB 返回底层 *sql.DB，供 executor 与 metadata 使用。
func (m *mysqlDataSource) DB() *sql.DB {
	return m.db
}

func buildMySQLDSN(cfg Config) (string, error) {
	if cfg.DSN != "" {
		parsed, err := mysqldriver.ParseDSN(cfg.DSN)
		if err != nil {
			return "", fmt.Errorf("mysql datasource: parse dsn for id=%s: %w", cfg.ID, err)
		}
		ensureNoMultiStatements(parsed)
		return parsed.FormatDSN(), nil
	}
	if cfg.Host == "" || cfg.User == "" || cfg.DBName == "" {
		return "", fmt.Errorf("mysql datasource: incomplete config for id=%s", cfg.ID)
	}
	port := cfg.Port
	if port == 0 {
		port = 3306
	}
	mc := mysqldriver.NewConfig()
	mc.User = cfg.User
	mc.Passwd = cfg.Password
	mc.Net = "tcp"
	mc.Addr = fmt.Sprintf("%s:%d", cfg.Host, port)
	mc.DBName = cfg.DBName
	mc.ParseTime = true
	mc.Params = map[string]string{
		"charset":         "utf8mb4,utf8",
		"multiStatements": "false",
	}
	return mc.FormatDSN(), nil
}

func ensureNoMultiStatements(cfg *mysqldriver.Config) {
	cfg.MultiStatements = false
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	for k := range cfg.Params {
		if strings.EqualFold(k, "multiStatements") {
			delete(cfg.Params, k)
		}
	}
	cfg.Params["multiStatements"] = "false"
}

// NewMySQLDataSource 根据 Config 打开 MySQL 连接并配置连接池。
func NewMySQLDataSource(cfg Config) (*mysqlDataSource, error) {
	if cfg.ID == "" {
		return nil, fmt.Errorf("mysql datasource: missing id")
	}
	dsn, err := buildMySQLDSN(cfg)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("mysql datasource: open failed for host=%s db=%s: %w", cfg.Host, cfg.DBName, err)
	}
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)
	}

	return &mysqlDataSource{
		id: cfg.ID,
		db: db,
	}, nil
}

// RegisterMySQL 在 Registry 上注册 "mysql" 类型的数据源工厂。
func RegisterMySQL(r *Registry) {
	if r == nil {
		return
	}
	r.RegisterType("mysql", func(cfg Config) (DataSource, error) {
		return NewMySQLDataSource(cfg)
	})
}
