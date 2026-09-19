package datasource

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestESHTTP_DoWithoutProductHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app-*/_search" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method=%s", r.Method)
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "query") {
			t.Errorf("body=%s", b)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"hits":{"total":1,"hits":[]}}`))
	}))
	defer srv.Close()
	c := &ESHTTP{BaseURL: srv.URL, Client: srv.Client()}
	st, body, err := c.Do(context.Background(), http.MethodPost, "/app-*/_search", []byte(`{"query":{"match_all":{}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if st != 200 || !strings.Contains(string(body), `"total":1`) {
		t.Fatalf("status=%d body=%s", st, body)
	}
}

func TestNewElasticsearchDataSource_UsesConfigHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 3 * time.Second}
	ds, err := NewElasticsearchDataSource(Config{
		ID:         "es-proxy",
		DSN:        "http://localhost:9200",
		HTTPClient: custom,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ds.ESHTTP() == nil || ds.ESHTTP().Client != custom {
		t.Fatal("want Config.HTTPClient assigned to ESHTTP.Client")
	}
}

func TestNewElasticsearchDataSource_NilHTTPClientKeepsTimeout(t *testing.T) {
	ds, err := NewElasticsearchDataSource(Config{ID: "es", DSN: "http://localhost:9200"})
	if err != nil {
		t.Fatal(err)
	}
	if ds.ESHTTP() == nil || ds.ESHTTP().Client == nil {
		t.Fatal("expected default HTTP client")
	}
	if ds.ESHTTP().Client.Timeout != defaultESHTTPTimeout {
		t.Fatalf("timeout=%v want %v", ds.ESHTTP().Client.Timeout, defaultESHTTPTimeout)
	}
}

func TestNewElasticsearchDataSource_Ping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"node-2","version":{"number":"6.8.23"},"tagline":"You Know, for Search"}`))
	}))
	defer srv.Close()
	ds, err := NewElasticsearchDataSource(Config{ID: "es", DSN: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := ds.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}
