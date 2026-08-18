package datasource

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultESHTTPTimeout = 60 * time.Second

// ESHTTPProvider is implemented by Elasticsearch datasources.
// Search/metadata use raw HTTP so ES 6.x / 7.x / 8.x and OpenSearch all work
// (official go-elasticsearch/v8 rejects servers without X-Elastic-Product).
type ESHTTPProvider interface {
	ESHTTP() *ESHTTP
}

// ESHTTP is a version-agnostic Elasticsearch/_search HTTP client.
type ESHTTP struct {
	BaseURL  string
	Username string
	Password string
	Client   *http.Client
}

func (c *ESHTTP) httpClient() *http.Client {
	if c != nil && c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: defaultESHTTPTimeout}
}

func (c *ESHTTP) url(relPath string) string {
	base := ""
	if c != nil {
		base = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	}
	rel := "/" + strings.TrimLeft(relPath, "/")
	if base == "" {
		return rel
	}
	return base + rel
}

// Do sends an HTTP request to the ES node. body may be nil.
func (c *ESHTTP) Do(ctx context.Context, method, relPath string, body []byte) (status int, respBody []byte, err error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return 0, nil, fmt.Errorf("elasticsearch: empty endpoint")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(relPath), rdr)
	if err != nil {
		return 0, nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Username != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
	res, err := c.httpClient().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, nil, fmt.Errorf("elasticsearch: read body: %w", err)
	}
	return res.StatusCode, respBody, nil
}
