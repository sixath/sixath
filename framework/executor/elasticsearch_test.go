package executor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/sixath/framework/datasource"
)

type esStubDS struct {
	id   string
	http *datasource.ESHTTP
}

func (e *esStubDS) ID() string                     { return e.id }
func (e *esStubDS) Type() string                   { return datasource.TypeElasticsearch }
func (e *esStubDS) Ping(ctx context.Context) error { return nil }
func (e *esStubDS) Close() error                   { return nil }
func (e *esStubDS) ESHTTP() *datasource.ESHTTP     { return e.http }

func mockESHTTP(t *testing.T, handler http.HandlerFunc) (*datasource.ESHTTP, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	return &datasource.ESHTTP{BaseURL: srv.URL, Client: srv.Client()}, srv
}

func mockESBody(t *testing.T, body string) (*datasource.ESHTTP, *httptest.Server) {
	t.Helper()
	return mockESHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
}

// 异构文档:首行无 error_code,后行有
const heterogeneousHits = `{
  "hits": {
    "total": {"value": 2},
    "hits": [
      {"_source": {"name": "alice"}},
      {"_source": {"name": "bob", "error_code": 500}}
    ]
  }
}`

func registerESExecutor(t *testing.T, client *datasource.ESHTTP) *ESExecutor {
	t.Helper()
	reg := datasource.NewRegistry()
	reg.RegisterType(datasource.TypeElasticsearch, func(cfg datasource.Config) (datasource.DataSource, error) {
		return &esStubDS{id: cfg.ID, http: client}, nil
	})
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: datasource.TypeElasticsearch}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return NewESExecutor(reg)
}

const basicSearchHits = `{
  "hits": {
    "total": {"value": 1},
    "hits": [{"_source": {"id": 1, "name": "alice"}}]
  }
}`

func TestESExecutor_BasicSearch(t *testing.T) {
	client, srv := mockESBody(t, basicSearchHits)
	defer srv.Close()
	ex := registerESExecutor(t, client)
	res, err := ex.Execute(context.Background(), "ds1", `{"query":{"match_all":{}}}`, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Columns) != 2 || len(res.Rows) != 1 {
		t.Fatalf("columns=%v rows=%d", res.Columns, len(res.Rows))
	}
	if res.Rows[0][1] != "alice" {
		t.Errorf("name = %v, want alice", res.Rows[0][1])
	}
}

func TestESExecutor_ES6TotalNoProductHeader(t *testing.T) {
	const es6 = `{
  "hits": {
    "total": 2,
    "hits": [
      {"_source": {"name": "alice"}},
      {"_source": {"name": "bob"}}
    ]
  }
}`
	var gotPath string
	client, srv := mockESHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get("X-Elastic-Product") != "" {
			t.Errorf("client must not require product header")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(es6))
	})
	defer srv.Close()
	ex := registerESExecutor(t, client)
	res, err := ex.Execute(context.Background(), "ds1", `{"query":{"match_all":{}}}`, ExecuteOptions{
		MaxRows: 10,
		Params:  map[string]any{"index": "app-logs-*"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/app-logs-*/_search" {
		t.Fatalf("path=%q", gotPath)
	}
	if res.EstimatedTotal != 2 {
		t.Fatalf("total=%d want 2", res.EstimatedTotal)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows=%d", len(res.Rows))
	}
}

func TestESExecutor_HeterogeneousColumnsUnion(t *testing.T) {
	client, srv := mockESBody(t, heterogeneousHits)
	defer srv.Close()

	ex := registerESExecutor(t, client)
	res, err := ex.Execute(context.Background(), "ds1", `{"query":{"match_all":{}}}`, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := []string{"error_code", "name"}
	if !reflect.DeepEqual(res.Columns, want) {
		t.Errorf("Columns = %v, want %v", res.Columns, want)
	}

	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(res.Rows))
	}
	if res.Rows[0][0] != nil {
		t.Errorf("row[0] error_code = %v, want nil", res.Rows[0][0])
	}
	if v, ok := res.Rows[1][0].(float64); !ok || v != 500 {
		t.Errorf("row[1] error_code = %v, want 500", res.Rows[1][0])
	}
}

func TestESExecutor_StableColumnOrder(t *testing.T) {
	client, srv := mockESBody(t, heterogeneousHits)
	defer srv.Close()

	ex := registerESExecutor(t, client)
	var first []string
	for i := 0; i < 50; i++ {
		res, err := ex.Execute(context.Background(), "ds1", `{"query":{"match_all":{}}}`, ExecuteOptions{})
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if first == nil {
			first = append([]string{}, res.Columns...)
			continue
		}
		if !reflect.DeepEqual(res.Columns, first) {
			t.Fatalf("columns drifted on iter %d: %v vs %v", i, res.Columns, first)
		}
	}
}

const queryShardException = `{"error":{"type":"query_shard_exception","reason":"No mapping found for [foo]"},"status":400}`
const clusterBlockException = `{"error":{"type":"cluster_block_exception","reason":"blocked"},"status":403}`

func mockESHTTPErr(t *testing.T, body string, status int) (*datasource.ESHTTP, *httptest.Server) {
	t.Helper()
	return mockESHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func TestESExecutor_SchemaErrorByType(t *testing.T) {
	client, srv := mockESHTTPErr(t, queryShardException, http.StatusBadRequest)
	defer srv.Close()
	reg := datasource.NewRegistry()
	reg.RegisterType("es", func(cfg datasource.Config) (datasource.DataSource, error) {
		return &esStubDS{id: cfg.ID, http: client}, nil
	})
	_, _ = reg.Register(datasource.Config{ID: "ds1", Type: "es"})

	ex := NewESExecutor(reg)
	_, err := ex.Execute(context.Background(), "ds1", `{}`, ExecuteOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsSchemaRelated(err) {
		t.Errorf("expected SchemaRelatedError, got %v", err)
	}
}

func TestESExecutor_InjectSize(t *testing.T) {
	var gotBody string
	client, srv := mockESHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"hits":{"total":{"value":0},"hits":[]}}`))
	})
	defer srv.Close()

	ex := registerESExecutor(t, client)
	_, err := ex.Execute(context.Background(), "ds1", `{"query":{"match_all":{}}}`, ExecuteOptions{MaxRows: 25})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(gotBody), &m); err != nil {
		t.Fatalf("request body json: %v (%s)", err, gotBody)
	}
	sz, ok := m["size"].(float64)
	if !ok || int(sz) != 25 {
		t.Fatalf("request size = %v, want 25 (body %s)", m["size"], gotBody)
	}
}

func TestESExecutor_MaxRows(t *testing.T) {
	var gotBody string
	hits := make([]string, 100)
	for i := range hits {
		hits[i] = `{"_source":{"n":` + strconv.Itoa(i) + `}}`
	}
	respBody := `{"hits":{"total":{"value":100},"hits":[` + strings.Join(hits, ",") + `]}}`
	client, srv := mockESHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(respBody))
	})
	defer srv.Close()
	ex := registerESExecutor(t, client)
	res, err := ex.Execute(context.Background(), "ds1", `{"query":{"match_all":{}}}`, ExecuteOptions{MaxRows: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Rows) != 10 {
		t.Fatalf("rows = %d, want 10", len(res.Rows))
	}
	if !res.Truncated {
		t.Fatal("expected Truncated=true")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(gotBody), &m); err != nil {
		t.Fatalf("body: %v", err)
	}
	if int(m["size"].(float64)) != 10 {
		t.Fatalf("request size = %v, want 10", m["size"])
	}
}

func TestESExecutor_UnsupportedDataSource(t *testing.T) {
	reg := datasource.NewRegistry()
	datasource.RegisterNoop(reg)
	if _, err := reg.Register(datasource.Config{ID: "noop", Type: "noop"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ex := NewESExecutor(reg)
	_, err := ex.Execute(context.Background(), "noop", `{}`, ExecuteOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrUnsupportedDataSource) {
		t.Fatalf("got %v", err)
	}
}

func TestESExecutor_InvalidJSON(t *testing.T) {
	client, srv := mockESBody(t, `{"hits":{"total":{"value":0},"hits":[]}}`)
	defer srv.Close()
	ex := registerESExecutor(t, client)
	_, err := ex.Execute(context.Background(), "ds1", `{invalid`, ExecuteOptions{MaxRows: 1})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestESExecutor_QueryForwardsIndex(t *testing.T) {
	var path string
	client, srv := mockESHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"hits":{"total":0,"hits":[]}}`))
	})
	defer srv.Close()
	ex := registerESExecutor(t, client)
	_, err := ex.Query(context.Background(), "ds1", `{"query":{"match_all":{}}}`, QueryOptions{
		MaxRows: 5,
		Extras:  map[string]any{"index": "backend-union-*"},
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if path != "/backend-union-*/_search" {
		t.Fatalf("path=%q", path)
	}
}

func TestESExecutor_IndexParam(t *testing.T) {
	var path string
	client, srv := mockESHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"hits":{"total":{"value":0},"hits":[]}}`))
	})
	defer srv.Close()
	ex := registerESExecutor(t, client)
	_, err := ex.Execute(context.Background(), "ds1", `{"query":{"match_all":{}}}`, ExecuteOptions{
		Params: map[string]any{"index": "logs-2024,metrics"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(path, "logs-2024") || !strings.Contains(path, "metrics") {
		t.Fatalf("path = %q, want both indices", path)
	}
}

func TestESExecutor_NonSchemaError(t *testing.T) {
	client, srv := mockESHTTPErr(t, clusterBlockException, http.StatusForbidden)
	defer srv.Close()
	reg := datasource.NewRegistry()
	reg.RegisterType("es", func(cfg datasource.Config) (datasource.DataSource, error) {
		return &esStubDS{id: cfg.ID, http: client}, nil
	})
	_, _ = reg.Register(datasource.Config{ID: "ds1", Type: "es"})

	ex := NewESExecutor(reg)
	_, err := ex.Execute(context.Background(), "ds1", `{}`, ExecuteOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if IsSchemaRelated(err) {
		t.Errorf("did not expect SchemaRelatedError, got %v", err)
	}
}
