package templates

import (
	"testing"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/tool"
)

func hasTool(reg *tool.Registry, name string) bool {
	_, ok := reg.Get(name)
	return ok
}

func TestRegisterRCATools_AllConfigured(t *testing.T) {
	cfg := config.Config{
		DataSources: []datasource.Config{
			{ID: "es-logs", Type: "elasticsearch", DSN: "http://localhost:9200"},
		},
		RCA: config.RCAConfig{
			Jaeger: config.RCAJaegerConfig{QueryURL: "http://jaeger:16686"},
			ES:     config.RCAESConfig{DatasourceID: "es-logs", DefaultIndex: "app-logs-*", TraceIDField: "trace_id"},
			Repos:  config.RCAReposConfig{Roots: []string{"/repos/a", "/repos/b"}},
		},
	}
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, cfg); err != nil {
		t.Fatalf("registerRCATools: %v", err)
	}
	for _, n := range []string{"rca_grep", "rca_glob", "rca_read", "jaeger_trace", "es_log_query"} {
		if !hasTool(reg, n) {
			t.Fatalf("expected %s registered", n)
		}
	}
}

func TestRegisterRCATools_PartialSkips(t *testing.T) {
	cfg := config.Config{
		RCA: config.RCAConfig{
			Repos: config.RCAReposConfig{Roots: []string{"/repos/a"}},
		},
	}
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, cfg); err != nil {
		t.Fatalf("registerRCATools: %v", err)
	}
	if !hasTool(reg, "rca_grep") {
		t.Fatal("rca_grep should be registered when roots set")
	}
	if hasTool(reg, "jaeger_trace") {
		t.Fatal("jaeger_trace should be skipped when query_url empty")
	}
	if hasTool(reg, "es_log_query") {
		t.Fatal("es_log_query should be skipped when datasource_id empty")
	}
}

func TestRegisterRCATools_Empty(t *testing.T) {
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, config.Config{}); err != nil {
		t.Fatalf("empty config must not error: %v", err)
	}
	if hasTool(reg, "rca_grep") || hasTool(reg, "jaeger_trace") || hasTool(reg, "es_log_query") {
		t.Fatal("no RCA tools should register with empty config")
	}
}

func TestRegisterRCATools_ESInlineEndpoint(t *testing.T) {
	cfg := config.Config{
		RCA: config.RCAConfig{
			ES: config.RCAESConfig{Endpoint: "http://localhost:9200", DefaultIndex: "app-*"},
		},
	}
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, cfg); err != nil {
		t.Fatalf("registerRCATools: %v", err)
	}
	if !hasTool(reg, "es_log_query") {
		t.Fatal("inline endpoint should register es_log_query")
	}
}

func TestRegisterRCATools_ESBothSkip(t *testing.T) {
	cfg := config.Config{
		DataSources: []datasource.Config{
			{ID: "es-logs", Type: "elasticsearch", DSN: "http://localhost:9200"},
		},
		RCA: config.RCAConfig{
			ES: config.RCAESConfig{Endpoint: "http://localhost:9200", DatasourceID: "es-logs"},
		},
	}
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, cfg); err != nil {
		t.Fatalf("registerRCATools: %v", err)
	}
	if hasTool(reg, "es_log_query") {
		t.Fatal("both endpoint and datasource_id must skip es_log_query")
	}
}
