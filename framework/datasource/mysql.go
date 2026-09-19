package datasource

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode"

	_ "github.com/go-sql-driver/mysql"
	mysqldriver "github.com/go-sql-driver/mysql"
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
		applyMySQLDialNet(parsed, cfg)
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
	applyMySQLDialNet(mc, cfg)
	mc.Addr = fmt.Sprintf("%s:%d", cfg.Host, port)
	mc.DBName = cfg.DBName
	mc.ParseTime = true
	mc.Params = map[string]string{
		"charset":         "utf8mb4,utf8",
		"multiStatements": "false",
	}
	return mc.FormatDSN(), nil
}

func mysqlProxyNetName(cfg Config) string {
	if key := strings.TrimSpace(cfg.ProxyNetKey); key != "" {
		return sanitizeMySQLNetID(cfg.ID) + "-" + sanitizeMySQLNetID(key)
	}
	return "sixath-proxy-" + sanitizeMySQLNetID(cfg.ID)
}

func sanitizeMySQLNetID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}

func applyMySQLDialNet(parsed *mysqldriver.Config, cfg Config) {
	if cfg.DialContext == nil {
		return
	}
	parsed.Net = mysqlProxyNetName(cfg)
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
	if cfg.DialContext != nil {
		netName := mysqlProxyNetName(cfg)
		dial := cfg.DialContext
		mysqldriver.RegisterDialContext(netName, func(ctx context.Context, addr string) (net.Conn, error) {
			conn, err := dial(ctx, "tcp", addr)
			if err != nil {
				return nil, fmt.Errorf("mysql datasource: dial failed for id=%s: %w", cfg.ID, err)
			}
			return conn, nil
		})
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
