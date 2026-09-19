package templates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/netx"
	"github.com/sixath/framework/tool"
)

func hasTool(reg *tool.Registry, name string) bool {
	_, ok := reg.Get(name)
	return ok
}

func workspaceWithCode(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, "code"), 0o755); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestRegisterRCATools_AllConfigured(t *testing.T) {
	cfg := config.Config{
		Workspace: workspaceWithCode(t),
		DataSources: []datasource.Config{
			{ID: "es-logs", Type: "elasticsearch", DSN: "http://localhost:9200"},
		},
		RCA: config.RCAConfig{
			Jaeger: config.RCAJaegerConfig{QueryURL: "http://jaeger:16686"},
			ES:     config.RCAESConfig{DatasourceID: "es-logs", DefaultIndex: "app-logs-*", TraceIDField: "trace_id", BodyField: "message"},
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
	if hasTool(reg, "vm_run_cmd") {
		t.Fatal("vm_run_cmd must stay off unless rca.vm_run_cmd.enabled")
	}
	es, ok := reg.Get("es_log_query")
	if !ok {
		t.Fatal("es_log_query missing")
	}
	if !strings.Contains(es.Description, "body field `message`") {
		t.Fatalf("YAML body_field not wired into es_log_query: %s", es.Description)
	}
}

func TestRegisterRCATools_IgnoresReposRootsWithoutMount(t *testing.T) {
	cfg := config.Config{
		RCA: config.RCAConfig{
			Repos: config.RCAReposConfig{Roots: []string{"/repos/a"}},
		},
	}
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, cfg); err != nil {
		t.Fatalf("registerRCATools: %v", err)
	}
	if hasTool(reg, "rca_grep") {
		t.Fatal("rca_grep must not register from rca.repos.roots without workspace/code")
	}
}

func TestRegisterRCATools_PartialSkips(t *testing.T) {
	cfg := config.Config{
		Workspace: workspaceWithCode(t),
		RCA: config.RCAConfig{
			Repos: config.RCAReposConfig{Roots: []string{"/repos/a"}},
		},
	}
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, cfg); err != nil {
		t.Fatalf("registerRCATools: %v", err)
	}
	if !hasTool(reg, "rca_grep") {
		t.Fatal("rca_grep should be registered when workspace/code exists")
	}
	if hasTool(reg, "jaeger_trace") {
		t.Fatal("jaeger_trace should be skipped when query_url empty")
	}
	if hasTool(reg, "es_log_query") {
		t.Fatal("es_log_query should be skipped when datasource_id empty")
	}
	if hasTool(reg, "vm_run_cmd") {
		t.Fatal("vm_run_cmd should be skipped when not enabled")
	}
}

func TestRegisterRCATools_Empty(t *testing.T) {
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, config.Config{}); err != nil {
		t.Fatalf("empty config must not error: %v", err)
	}
	if hasTool(reg, "rca_grep") || hasTool(reg, "jaeger_trace") || hasTool(reg, "es_log_query") || hasTool(reg, "vm_run_cmd") {
		t.Fatal("no RCA tools should register with empty config")
	}
}

func TestRegisterRCATools_VMRunCmdEnabled(t *testing.T) {
	cfg := config.Config{
		DataSources: []datasource.Config{
			{ID: "game-mysql", Type: datasource.TypeMySQL, DSN: "user:pass@tcp(127.0.0.1:3306)/game"},
		},
		RCA: config.RCAConfig{
			VMRunCmd: config.RCAVMRunCmdConfig{Enabled: true, DatasourceID: "game-mysql"},
		},
	}
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, cfg); err != nil {
		t.Fatalf("registerRCATools: %v", err)
	}
	if !hasTool(reg, "vm_run_cmd") {
		t.Fatal("enabled:true must register vm_run_cmd")
	}
}

func TestRegisterRCATools_VMRunCmdHostPostsWithoutProxy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		RCA: config.RCAConfig{
			VMRunCmd: config.RCAVMRunCmdConfig{Enabled: true},
		},
	}
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, cfg); err != nil {
		t.Fatalf("registerRCATools: %v", err)
	}
	tl, ok := reg.Get("vm_run_cmd")
	if !ok {
		t.Fatal("expected vm_run_cmd")
	}
	out, err := tl.Execute(context.Background(), map[string]any{
		"cmd":  "Get-Process",
		"host": u.Hostname(),
		"port": port,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m, _ := out.(map[string]any)
	if msg, _ := m["error"].(string); strings.Contains(msg, "egress client unavailable") {
		t.Fatalf("direct/no proxy must not be egress unavailable: %#v", m)
	}
	if m["ok"] != true {
		t.Fatalf("want ok=true without Proxies, got %#v", m)
	}
}

func TestRegisterRCATools_VMRunCmdUnknownProxyFailClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("unknown proxy_id must not fall back to a silent direct client")
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		ProxyID: "ghost",
		RCA: config.RCAConfig{
			VMRunCmd: config.RCAVMRunCmdConfig{Enabled: true},
		},
	}
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, cfg); err != nil {
		t.Fatalf("registerRCATools: %v", err)
	}
	tl, ok := reg.Get("vm_run_cmd")
	if !ok {
		t.Fatal("expected vm_run_cmd")
	}
	out, err := tl.Execute(context.Background(), map[string]any{
		"cmd":  "Get-Process",
		"host": u.Hostname(),
		"port": port,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m, _ := out.(map[string]any)
	if m["ok"] == true {
		t.Fatal("unknown proxy_id must fail-closed")
	}
	msg, _ := m["error"].(string)
	if !strings.Contains(msg, `proxy "ghost" not found`) {
		t.Fatalf("want proxy not found, got %#v", m)
	}
}

func TestRegisterRCATools_VMRunCmdDisabled(t *testing.T) {
	cfg := config.Config{
		RCA: config.RCAConfig{
			VMRunCmd: config.RCAVMRunCmdConfig{Enabled: false, DatasourceID: "game-mysql"},
		},
	}
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, cfg); err != nil {
		t.Fatalf("registerRCATools: %v", err)
	}
	if hasTool(reg, "vm_run_cmd") {
		t.Fatal("enabled:false must not register vm_run_cmd")
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

func TestRegisterRCATools_InjectsProxyClient(t *testing.T) {
	saw := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer proxy.Close()
	u, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		ProxyID: "office",
		Proxies: []netx.Spec{{
			ID:   "office",
			Type: netx.TypeHTTP,
			Host: u.Hostname(),
			Port: port,
		}},
		RCA: config.RCAConfig{
			Jaeger: config.RCAJaegerConfig{QueryURL: "http://jaeger.example:16686"},
		},
	}
	reg := tool.NewRegistry()
	if err := registerRCATools(reg, cfg); err != nil {
		t.Fatalf("registerRCATools: %v", err)
	}
	tl, ok := reg.Get("jaeger_trace")
	if !ok {
		t.Fatal("jaeger_trace should register")
	}
	_, _ = tl.Execute(context.Background(), map[string]any{"trace_id": "abc"})
	if !saw {
		t.Fatal("jaeger client must be injected from Config.Proxies/ProxyID")
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
