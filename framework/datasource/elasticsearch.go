package datasource

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// esDataSource 实现 DataSource 与 ESHTTPProvider。
type esDataSource struct {
	id   string
	http *ESHTTP
}

func (e *esDataSource) ID() string   { return e.id }
func (e *esDataSource) Type() string { return TypeElasticsearch }

func (e *esDataSource) Ping(ctx context.Context) error {
	status, body, err := e.http.Do(ctx, http.MethodGet, "/", nil)
	if err != nil {
		return fmt.Errorf("elasticsearch ping: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("elasticsearch ping: HTTP %d %s", status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (e *esDataSource) Close() error {
	return nil
}

func (e *esDataSource) ESHTTP() *ESHTTP { return e.http }

// NewElasticsearchDataSource 根据 Config 创建 Elasticsearch 数据源。
// 使用 DSN 作为完整 URL（如 http://localhost:9200），若为空则用 Host:Port（默认 9200）。
// 可选 User/Password 用于 Basic 认证。走裸 HTTP，不依赖官方 go-elasticsearch 客户端。
func NewElasticsearchDataSource(cfg Config) (*esDataSource, error) {
	if cfg.ID == "" {
		return nil, fmt.Errorf("elasticsearch datasource: missing id")
	}
	addr := cfg.DSN
	if addr == "" {
		host := cfg.Host
		if host == "" {
			host = "localhost"
		}
		port := cfg.Port
		if port == 0 {
			port = 9200
		}
		addr = fmt.Sprintf("http://%s:%d", host, port)
	}
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}

	return &esDataSource{
		id: cfg.ID,
		http: &ESHTTP{
			BaseURL:  strings.TrimRight(addr, "/"),
			Username: cfg.User,
			Password: cfg.Password,
			Client:   &http.Client{Timeout: defaultESHTTPTimeout},
		},
	}, nil
}

// RegisterElasticsearch 在 Registry 上注册 "elasticsearch" 类型的数据源工厂。
func RegisterElasticsearch(r *Registry) {
	if r == nil {
		return
	}
	r.RegisterType("elasticsearch", func(cfg Config) (DataSource, error) {
		return NewElasticsearchDataSource(cfg)
	})
}
