package datasource

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDatabaseProvider 由 MongoDB 数据源实现，供 metadata 与 executor 使用。
type MongoDatabaseProvider interface {
	MongoDatabase() *mongo.Database
}

// mongoDataSource 实现 DataSource 与 MongoDatabaseProvider。
type mongoDataSource struct {
	id string
	db *mongo.Database
}

func (m *mongoDataSource) ID() string   { return m.id }
func (m *mongoDataSource) Type() string { return TypeMongoDB }

func (m *mongoDataSource) Ping(ctx context.Context) error {
	return m.db.Client().Ping(ctx, nil)
}

func (m *mongoDataSource) Close() error {
	return m.db.Client().Disconnect(context.Background())
}

// MongoDatabase 返回底层 *mongo.Database，供 executor 与 metadata 使用。
func (m *mongoDataSource) MongoDatabase() *mongo.Database {
	return m.db
}

// NewMongoDataSource 根据 Config 创建 MongoDB 数据源。
// DSN 形如 mongodb://user:pass@host:port/dbname?authSource=admin；
// 若 DSN 为空则使用 Host/Port/User/Password/DBName 组装。
// 密码中的 @ 会做百分号编码；分字段组装时认证库默认 admin（可用 AuthSource 覆盖）。
func NewMongoDataSource(cfg Config) (*mongoDataSource, error) {
	if cfg.ID == "" {
		return nil, fmt.Errorf("mongodb datasource: missing id")
	}
	if cfg.DBName == "" {
		return nil, fmt.Errorf("mongodb datasource: missing dbname for id=%s", cfg.ID)
	}

	uri := strings.TrimSpace(cfg.DSN)
	if uri == "" {
		host := cfg.Host
		if host == "" {
			host = "localhost"
		}
		port := cfg.Port
		if port == 0 {
			port = 27017
		}
		uri = buildMongoURI(host, port, cfg.User, cfg.Password, cfg.DBName, mongoAuthSource(cfg))
	} else {
		uri = encodeMongoURIUserinfo(uri)
		uri = ensureMongoAuthSource(uri, mongoAuthSource(cfg))
	}

	clientOpts := options.Client().ApplyURI(uri)
	if cfg.MaxOpenConns > 0 {
		// 将 MaxOpenConns 粗略映射为连接池大小
		clientOpts.SetMaxPoolSize(uint64(cfg.MaxOpenConns))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("mongodb datasource: connect: %w", err)
	}

	db := client.Database(cfg.DBName)
	return &mongoDataSource{id: cfg.ID, db: db}, nil
}

func mongoAuthSource(cfg Config) string {
	if s := strings.TrimSpace(cfg.AuthSource); s != "" {
		return s
	}
	if strings.TrimSpace(cfg.User) != "" || strings.TrimSpace(cfg.DSN) != "" {
		return "admin"
	}
	return ""
}

// buildMongoURI 用 net/url 组装连接串，对 user/password 做百分号编码（避免密码中的 @、: 等破坏 URI）。
// authSource 对应 mongo shell 的 --authenticationDatabase；有账号时默认 admin。
func buildMongoURI(host string, port int, user, password, dbName, authSource string) string {
	u := &url.URL{
		Scheme: "mongodb",
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   "/" + dbName,
	}
	if user != "" {
		u.User = url.UserPassword(user, password)
	}
	q := u.Query()
	q.Set("readPreference", "secondaryPreferred")
	if authSource != "" {
		q.Set("authSource", authSource)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// encodeMongoURIUserinfo 把 userinfo 里未转义的 @ : / 等编进密码，避免 ApplyURI 直接失败。
func encodeMongoURIUserinfo(uri string) string {
	scheme, rest, ok := strings.Cut(uri, "://")
	if !ok {
		return uri
	}
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return uri
	}
	userinfo, hostpart := rest[:at], rest[at+1:]
	user, pass, hasPass := strings.Cut(userinfo, ":")
	if !hasPass || !strings.ContainsAny(pass, "@:/?#[]") {
		return uri
	}
	return scheme + "://" + url.UserPassword(user, pass).String() + "@" + hostpart
}

func ensureMongoAuthSource(uri, authSource string) string {
	if strings.TrimSpace(authSource) == "" {
		return uri
	}
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	q := u.Query()
	if q.Get("authSource") != "" || q.Get("authsource") != "" {
		return uri
	}
	q.Set("authSource", authSource)
	u.RawQuery = q.Encode()
	return u.String()
}

// RegisterMongoDB 在 Registry 上注册 "mongodb" 类型的数据源工厂。
func RegisterMongoDB(r *Registry) {
	if r == nil {
		return
	}
	r.RegisterType("mongodb", func(cfg Config) (DataSource, error) {
		return NewMongoDataSource(cfg)
	})
}
