package chat

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/biz"

	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/netx"
	"github.com/sixath/framework/tool"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestBuildSkillsIndexMergesWorkspaceAndExtraDirectories(t *testing.T) {
	workspace := t.TempDir()
	workspaceSkill := filepath.Join(workspace, "skills", "workspace-skill", "SKILL.md")
	sharedSkillDir := filepath.Join(t.TempDir(), "shared-skill")
	sharedSkill := filepath.Join(sharedSkillDir, "SKILL.md")

	for path, content := range map[string]string{
		workspaceSkill: "---\nname: workspace-skill\ndescription: workspace\n---\n",
		sharedSkill:    "---\nname: shared-skill\ndescription: shared\n---\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	index, err := BuildSkillsIndex(workspace, []string{sharedSkillDir})
	if err != nil {
		t.Fatalf("BuildSkillsIndex: %v", err)
	}
	if index == nil {
		t.Fatal("BuildSkillsIndex returned nil index")
	}
	for _, name := range []string{"workspace-skill", "shared-skill"} {
		if _, ok := index.GetByName(name); !ok {
			t.Errorf("skill %q missing from index", name)
		}
	}
}

func TestBuildRegistry_NoAgentToolsKeepsRegistryDefaults(t *testing.T) {
	reg := tool.NewRegistry()
	_, err := BuildRegistry(nil, nil, reg)
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if _, ok := reg.Get("http_request"); !ok {
		t.Fatal("expected default http_request from tool.NewRegistry when agent has no tools")
	}
}

func TestBuildRegistry_AllDatasourcesFailIncludesDetail(t *testing.T) {
	cfg, err := structpb.NewStruct(map[string]interface{}{
		"datasource": map[string]interface{}{
			"type": "hive",
		},
	})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	reg := tool.NewRegistry()
	_, err = BuildRegistry([]*biz.ToolMeta{{
		ID:     "ds-1",
		Name:   "bad-hive",
		Type:   biz.ToolTypeDatasource,
		Config: cfg,
	}}, nil, reg)
	if err == nil {
		t.Fatal("expected error when datasource cannot register")
	}
	if !containsAll(err.Error(), "所有数据源均注册失败", "bad-hive", "incomplete") {
		t.Fatalf("error should include datasource detail, got: %v", err)
	}
}

func TestBuildRegistry_ElasticsearchOnly_NoDataTrio(t *testing.T) {
	cfg, err := structpb.NewStruct(map[string]interface{}{
		"datasource": map[string]interface{}{
			"id":            "zj-es",
			"type":          "elasticsearch",
			"dsn":           "http://127.0.0.1:9200",
			"default_index": "app-*",
			"purpose":       "应用日志",
		},
	})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	reg := tool.NewRegistry()
	res, err := BuildRegistry([]*biz.ToolMeta{{
		ID:     "es-1",
		Name:   "zj-es",
		Type:   biz.ToolTypeDatasource,
		Config: cfg,
	}}, nil, reg)
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	for _, name := range []string{"list_tables", "describe_table", "execute_read"} {
		if _, ok := reg.Get(name); ok {
			t.Fatalf("ES-only agent must not register %s", name)
		}
	}
	if res == nil || !strings.Contains(res.DatasourcePrompt, "es_log_query") {
		t.Fatalf("prompt=%q", res.DatasourcePrompt)
	}
	if !strings.Contains(res.DatasourcePrompt, "cluster=zj-es") {
		t.Fatalf("want cluster=<toolname> in prompt, got %q", res.DatasourcePrompt)
	}
	if !strings.Contains(res.DatasourcePrompt, "应用日志") || !strings.Contains(res.DatasourcePrompt, "app-*") {
		t.Fatalf("want purpose/default_index from tool config map, got %q", res.DatasourcePrompt)
	}
	if strings.Contains(res.DatasourcePrompt, "**zj-es**") {
		t.Fatalf("ES id must not appear in data trio list: %s", res.DatasourcePrompt)
	}
	if len(res.DsBindings) != 1 || !res.DsBindings[0].SkipDataTools {
		t.Fatalf("bindings=%+v", res.DsBindings)
	}
	if res.DsBindings[0].Purpose != "应用日志" || res.DsBindings[0].DefaultIndex != "app-*" {
		t.Fatalf("bindings must copy purpose/index from map, got %+v", res.DsBindings[0])
	}
	if _, ok := reg.Get("es_log_query"); !ok {
		t.Fatal("ES-only agent must register es_log_query")
	}
}

func TestBuildRegistry_RegistersAllBoundRCATools(t *testing.T) {
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, WorkspaceCodeLink), 0o755); err != nil {
		t.Fatal(err)
	}
	codeTool := &biz.ToolMeta{Name: "migu-rca", Type: biz.ToolTypeRCA, Config: mustRCAStruct(t, "rca_code", map[string]any{
		"roots": []any{t.TempDir()},
	})}
	esTool := &biz.ToolMeta{Name: "mg-rca-es", Type: biz.ToolTypeRCA, Config: mustRCAStruct(t, "es_log_query", map[string]any{
		"endpoint": "http://es",
	})}
	reg := tool.NewRegistry()
	if _, err := BuildRegistry([]*biz.ToolMeta{codeTool, esTool}, nil, reg, RegistryBuildOptions{Workspace: ws}); err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if _, ok := reg.Get("rca_grep"); !ok {
		t.Fatal("code RCA tools must register without a family surface")
	}
	if _, ok := reg.Get("es_log_query"); !ok {
		t.Fatal("es RCA tools must register without a family surface")
	}
}

func TestBuildRegistry_MySQLPlusES_RegistersDataTrioForMySQLOnly(t *testing.T) {
	mysqlCfg, err := structpb.NewStruct(map[string]interface{}{
		"datasource": map[string]interface{}{
			"id":     "cgarchive",
			"type":   "mysql",
			"dsn":    "user:pass@tcp(127.0.0.1:3306)/db",
			"dbname": "db",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	esCfg, err := structpb.NewStruct(map[string]interface{}{
		"datasource": map[string]interface{}{
			"id":   "zj-es",
			"type": "elasticsearch",
			"dsn":  "http://127.0.0.1:9200",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	res, err := BuildRegistry([]*biz.ToolMeta{
		{ID: "m1", Name: "cgarchive", Type: biz.ToolTypeDatasource, Config: mysqlCfg},
		{ID: "e1", Name: "zj-es", Type: biz.ToolTypeDatasource, Config: esCfg},
	}, nil, reg)
	if err != nil {
		// MySQL DSN may fail ping/register depending on factory — if so skip trio assert
		if strings.Contains(err.Error(), "所有数据源均注册失败") {
			t.Skipf("mysql register failed in env: %v", err)
		}
		t.Fatalf("BuildRegistry: %v", err)
	}
	if _, ok := reg.Get("execute_read"); !ok {
		t.Fatal("expected execute_read when mysql registers")
	}
	if strings.Contains(res.DatasourcePrompt, "**zj-es**") {
		t.Fatalf("ES must not be in data prompt list: %s", res.DatasourcePrompt)
	}
	if !strings.Contains(res.DatasourcePrompt, "es_log_query") {
		t.Fatalf("missing ES hint: %s", res.DatasourcePrompt)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func TestBuildRegistry_RegisterSSHExecBuiltin(t *testing.T) {
	cfg, err := structpb.NewStruct(map[string]interface{}{
		"func_path": "ssh_exec",
		"parameters": map[string]interface{}{
			"default_user":             "vrviu",
			"allowed_hosts":            []interface{}{"10.18.240.0/24"},
			"allowed_users":            []interface{}{"vrviu"},
			"allowed_command_prefixes": []interface{}{"journalctl -u archive-manager"},
			"strict_host_key_checking": "yes",
			"timeout_sec":              3,
		},
	})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	reg := tool.NewRegistry()
	_, err = BuildRegistry([]*biz.ToolMeta{{
		ID:     "ssh-tool",
		Name:   "SSH Exec",
		Type:   biz.ToolTypeBuiltin,
		Config: cfg,
	}}, nil, reg)
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	tl, ok := reg.Get("ssh_exec")
	if !ok {
		t.Fatal("ssh_exec not registered")
	}

	out, err := tl.Execute(context.Background(), map[string]any{
		"host":    "10.18.241.1",
		"user":    "root",
		"command": "rm -rf /",
	})
	if err != nil {
		t.Fatalf("Execute should return policy result: %v", err)
	}
	res := out.(tool.SSHExecResult)
	if res.OK || res.ErrorCategory != tool.SSHExecErrorBlockedByPolicy {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestBuildRegistry_NoProxyUseACL(t *testing.T) {
	repo := &catalogOnlyProxyRepo{byID: map[string]*biz.ProxyMeta{
		"office": {
			ID:       "office",
			Name:     "office",
			Type:     "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Password: "secret",
			NoProxy:  []string{"es.local"},
		},
	}}
	ctx := context.Background()
	cat := LoadProxyCatalog(ctx, repo, "office", nil)
	if repo.aclCalled {
		t.Fatal("loadProxyCatalog must not call ACL or extra repo methods")
	}
	if len(cat) != 1 {
		t.Fatalf("catalog=%v want office", cat)
	}
	if cat["office"].Password != "secret" {
		t.Fatal("catalog must keep password from GetByID")
	}

	reg := tool.NewRegistry()
	if _, err := BuildRegistry(nil, nil, reg, RegistryBuildOptions{
		AgentProxyID: "office",
		Proxies:      cat,
	}); err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if repo.aclCalled {
		t.Fatal("BuildRegistry must not call ACL")
	}
	if reg.HTTPClient() == nil {
		t.Fatal("http_request overlay must be SetHTTPClient from agent proxy")
	}
}

func TestBuildRegistry_HTTPRequestOverlaySet(t *testing.T) {
	spec := netxSpecHTTP("office")
	reg := tool.NewRegistry()
	if _, err := BuildRegistry(nil, nil, reg, RegistryBuildOptions{
		AgentProxyID: "office",
		Proxies:      map[string]netx.Spec{"office": spec},
	}); err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	c := reg.HTTPClient()
	if c == nil {
		t.Fatal("expected registry HTTPClient overlay")
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatal("overlay Transport.Proxy must be set for HTTP agent proxy")
	}
	u, err := tr.Proxy(&http.Request{URL: mustURL(t, "http://target.example/path")})
	if err != nil || u == nil || u.Host != "127.0.0.1:8080" {
		t.Fatalf("proxy URL=%v err=%v", u, err)
	}
}

func TestBuildRegistry_MySQLAgentHTTPUnavailable(t *testing.T) {
	mysqlCfg, err := structpb.NewStruct(map[string]interface{}{
		"datasource": map[string]interface{}{
			"id":     "orders",
			"type":   "mysql",
			"dsn":    "user:pass@tcp(127.0.0.1:3306)/db",
			"dbname": "db",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	res, err := BuildRegistry([]*biz.ToolMeta{{
		ID:     "m1",
		Name:   "orders",
		Type:   biz.ToolTypeDatasource,
		Config: mysqlCfg,
	}}, nil, reg, RegistryBuildOptions{
		AgentProxyID: "office",
		Proxies:      map[string]netx.Spec{"office": netxSpecHTTP("office")},
	})
	if err != nil {
		t.Fatalf("BuildRegistry should keep going with binding.Err, got %v", err)
	}
	if res == nil || len(res.DsBindings) != 1 {
		t.Fatalf("bindings=%+v", res)
	}
	b := res.DsBindings[0]
	if b.Available {
		t.Fatal("mysql + agent HTTP proxy must not register")
	}
	if b.Err == "" {
		t.Fatal("want binding.Err")
	}
	if !strings.Contains(b.Err, "office") {
		t.Fatalf("binding.Err should mention proxy id, got %q", b.Err)
	}
	if strings.Contains(b.Err, "secret") {
		t.Fatalf("binding.Err must not contain password: %q", b.Err)
	}
	if _, ok := reg.Get("execute_read"); ok {
		t.Fatal("unavailable mysql must not register data trio")
	}
}

func TestBuildRegistry_MissingAgentProxyFailClosed(t *testing.T) {
	mysqlCfg, err := structpb.NewStruct(map[string]interface{}{
		"datasource": map[string]interface{}{
			"id":     "orders",
			"type":   "mysql",
			"dsn":    "user:pass@tcp(127.0.0.1:3306)/db",
			"dbname": "db",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	res, err := BuildRegistry([]*biz.ToolMeta{{
		ID:     "m1",
		Name:   "orders",
		Type:   biz.ToolTypeDatasource,
		Config: mysqlCfg,
	}}, nil, reg, RegistryBuildOptions{
		AgentProxyID: "missing-office",
		Proxies:      map[string]netx.Spec{},
	})
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}

	c := reg.HTTPClient()
	if c == nil {
		t.Fatal("missing agent proxy must overlay a fail-closed client, not leave default direct")
	}
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	resp, reqErr := c.Get(srv.URL)
	if reqErr == nil {
		resp.Body.Close()
		t.Fatal("fail-closed overlay must not succeed as a direct client")
	}
	if hit {
		t.Fatal("fail-closed overlay must not reach dest")
	}
	if !strings.Contains(reqErr.Error(), "missing-office") {
		t.Fatalf("error should mention proxy id, got %v", reqErr)
	}

	if res == nil || len(res.DsBindings) != 1 {
		t.Fatalf("bindings=%+v", res)
	}
	b := res.DsBindings[0]
	if b.Available {
		t.Fatal("mysql inherit with missing agent proxy must not register")
	}
	if b.Err == "" || !strings.Contains(b.Err, "missing-office") {
		t.Fatalf("want binding.Err mentioning proxy, got %q", b.Err)
	}
	if _, ok := reg.Get("execute_read"); ok {
		t.Fatal("unavailable mysql must not register data trio")
	}
}

func TestResolveEffective_InheritMissingAgentProxy(t *testing.T) {
	_, err := resolveEffective(netx.Binding{Mode: netx.ModeInherit}, RegistryBuildOptions{
		AgentProxyID: "gone",
		Proxies:      map[string]netx.Spec{},
	}, "es.local")
	if err == nil || !strings.Contains(err.Error(), "gone") {
		t.Fatalf("want miss err, got %v", err)
	}

	eff, err := resolveEffective(netx.Binding{Mode: netx.ModeOff}, RegistryBuildOptions{
		AgentProxyID: "gone",
		Proxies:      map[string]netx.Spec{},
	}, "es.local")
	if err != nil || eff != nil {
		t.Fatalf("ModeOff: %+v %v", eff, err)
	}

	eff, err = resolveEffective(netx.Binding{Mode: netx.ModeInherit}, RegistryBuildOptions{}, "es.local")
	if err != nil || eff != nil {
		t.Fatalf("empty AgentProxyID: %+v %v", eff, err)
	}
}

func TestApplyMCPHTTPClient_InheritMissingDoesNotInjectDirect(t *testing.T) {
	mc := &tool.McpConfig{Id: "http-mcp", Transport: "http", Endpoint: "http://example.com"}
	err := applyMCPHTTPClient(mc, netx.Binding{Mode: netx.ModeInherit}, RegistryBuildOptions{
		AgentProxyID: "gone",
		Proxies:      map[string]netx.Spec{},
	})
	if err == nil {
		t.Fatal("inherit miss must error so caller can skip")
	}
	if mc.HTTPClient != nil {
		t.Fatal("must not inject a client (nil would mean direct)")
	}
}

func TestResolveJaegerClient_InheritMissing(t *testing.T) {
	c, err := resolveJaegerClient("http://jaeger:16686", netx.Binding{Mode: netx.ModeInherit}, RegistryBuildOptions{
		AgentProxyID: "gone",
		Proxies:      map[string]netx.Spec{},
	})
	if err == nil || c != nil {
		t.Fatalf("want skip, got client=%v err=%v", c, err)
	}
}

func TestApplyDatasourceEgress_ESInheritMissing(t *testing.T) {
	cfg := datasource.Config{ID: "logs", Type: "elasticsearch", DSN: "http://es.local:9200"}
	err := applyDatasourceEgress(&cfg, netx.Binding{Mode: netx.ModeInherit}, RegistryBuildOptions{
		AgentProxyID: "gone",
		Proxies:      map[string]netx.Spec{},
	})
	if err == nil {
		t.Fatal("want inherit miss error")
	}
	if cfg.HTTPClient != nil {
		t.Fatal("must not inject a client (nil would mean direct)")
	}
}

func TestApplyDatasourceEgress_ModeOffDirectDespiteMissingAgent(t *testing.T) {
	cfg := datasource.Config{
		ID:     "orders",
		Type:   "mysql",
		DSN:    "user:pass@tcp(127.0.0.1:3306)/db",
		DBName: "db",
	}
	if err := applyDatasourceEgress(&cfg, netx.Binding{Mode: netx.ModeOff}, RegistryBuildOptions{
		AgentProxyID: "gone",
		Proxies:      map[string]netx.Spec{},
	}); err != nil {
		t.Fatalf("ModeOff must stay direct: %v", err)
	}
	if cfg.DialContext != nil {
		t.Fatal("ModeOff must not inject DialContext")
	}
}

func TestApplyDatasourceEgress_MongoAgentSOCKS5SetsDialContext(t *testing.T) {
	cfg := datasource.Config{
		ID:     "cgmongo",
		Type:   "mongodb",
		Host:   "127.0.0.1",
		Port:   27017,
		DBName: "app",
	}
	if err := applyDatasourceEgress(&cfg, netx.Binding{Mode: netx.ModeInherit}, RegistryBuildOptions{
		AgentProxyID: "lab",
		Proxies:      map[string]netx.Spec{"lab": netxSpecSOCKS5("lab")},
	}); err != nil {
		t.Fatalf("applyDatasourceEgress: %v", err)
	}
	if cfg.DialContext == nil {
		t.Fatal("mongodb + agent SOCKS5 must set Config.DialContext")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, dialErr := cfg.DialContext(ctx, "tcp", "127.0.0.1:1")
	if dialErr == nil {
		t.Fatal("expected socks dial error")
	}
	if !strings.Contains(dialErr.Error(), "lab") {
		t.Fatalf("annotated dial err should mention proxy id, got %v", dialErr)
	}
}

func TestApplyDatasourceEgress_MongoHTTPRejected(t *testing.T) {
	cfg := datasource.Config{
		ID:     "cgmongo",
		Type:   "mongodb",
		Host:   "127.0.0.1",
		Port:   27017,
		DBName: "app",
	}
	err := applyDatasourceEgress(&cfg, netx.Binding{Mode: netx.ModeInherit}, RegistryBuildOptions{
		AgentProxyID: "office",
		Proxies:      map[string]netx.Spec{"office": netxSpecHTTP("office")},
	})
	if err == nil {
		t.Fatal("mongodb + HTTP proxy must fail")
	}
	if !strings.Contains(err.Error(), "http") && !strings.Contains(err.Error(), "office") {
		t.Fatalf("want http/proxy error, got %v", err)
	}
	if cfg.DialContext != nil {
		t.Fatal("must not inject DialContext for HTTP proxy")
	}
}

func TestApplyDatasourceEgress_HiveStaysDirect(t *testing.T) {
	cfg := datasource.Config{ID: "hv", Type: "hive", Host: "h", Port: 10000, DBName: "d"}
	if err := applyDatasourceEgress(&cfg, netx.Binding{Mode: netx.ModeInherit}, RegistryBuildOptions{
		AgentProxyID: "lab",
		Proxies:      map[string]netx.Spec{"lab": netxSpecSOCKS5("lab")},
	}); err != nil {
		t.Fatalf("hive inherit must stay direct: %v", err)
	}
	if cfg.DialContext != nil {
		t.Fatal("hive must not inject DialContext")
	}
}

func TestApplyDatasourceEgress_MySQLAgentSOCKS5SetsDialContext(t *testing.T) {
	cfg := datasource.Config{
		ID:     "orders",
		Type:   "mysql",
		DSN:    "user:pass@tcp(127.0.0.1:3306)/db",
		DBName: "db",
	}
	if err := applyDatasourceEgress(&cfg, netx.Binding{Mode: netx.ModeInherit}, RegistryBuildOptions{
		AgentProxyID: "lab",
		Proxies:      map[string]netx.Spec{"lab": netxSpecSOCKS5("lab")},
	}); err != nil {
		t.Fatalf("applyDatasourceEgress: %v", err)
	}
	if cfg.DialContext == nil {
		t.Fatal("mysql + agent SOCKS5 must set Config.DialContext")
	}
	if cfg.ProxyNetKey != "lab" {
		t.Fatalf("ProxyNetKey=%q want lab", cfg.ProxyNetKey)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, dialErr := cfg.DialContext(ctx, "tcp", "127.0.0.1:1")
	if dialErr == nil {
		t.Fatal("expected socks dial error")
	}
	if !strings.Contains(dialErr.Error(), "lab") {
		t.Fatalf("annotated dial err should mention proxy id, got %v", dialErr)
	}
}

func TestRegisterBoundMCPServers_SkipsWhenAlreadyMarked(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits++
	}))
	defer srv.Close()

	reg := tool.NewRegistry()
	reg.MarkMcpServer("fixture")
	entries, err := registerBoundMCPServers(reg, []*biz.McpServerMeta{{
		ID:        "fixture",
		Name:      "fixture-from-servers",
		Transport: "http",
		Endpoint:  srv.URL,
	}}, RegistryBuildOptions{})
	if err != nil {
		t.Fatalf("registerBoundMCPServers: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("pre-marked server must be skipped, got %d entries", len(entries))
	}
	if hits != 0 {
		t.Fatalf("skip must happen before RegisterMcpTool; httptest hits=%d want 0", hits)
	}
}

func TestBuildRegistry_MCPToolRowClientWins(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	fixture := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "framework", "tool", "testdata", "mcp_stdio_fixture.js"))
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("fixture missing at %s: %v", fixture, err)
	}

	toolCfg, err := structpb.NewStruct(map[string]interface{}{
		"egress_mode": "proxy",
		"proxy_id":    "lab",
		"mcp": map[string]interface{}{
			"id":        "fixture",
			"transport": "stdio",
			"command":   "node",
			"args":      []interface{}{fixture},
			"backend":   "mark3labs",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits++
	}))
	defer srv.Close()

	reg := tool.NewRegistry()
	if _, err := BuildRegistry([]*biz.ToolMeta{{
		ID:     "mcp-row",
		Name:   "fixture",
		Type:   biz.ToolTypeMCP,
		Config: toolCfg,
	}}, []*biz.McpServerMeta{{
		ID:        "fixture",
		Name:      "fixture-from-servers",
		Transport: "http",
		Endpoint:  srv.URL,
	}}, reg, RegistryBuildOptions{
		AgentProxyID: "office",
		Proxies: map[string]netx.Spec{
			"office": netxSpecHTTP("office"),
			"lab":    netxSpecHTTP("lab"),
		},
	}); err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if !reg.HasMcpServer("fixture") {
		t.Fatal("tool row must register MCP server")
	}
	if hits != 0 {
		t.Fatalf("servers loop must skip HasMcpServer; httptest hits=%d want 0", hits)
	}
}

func netxSpecHTTP(id string) netx.Spec {
	return netx.Spec{ID: id, Type: netx.TypeHTTP, Host: "127.0.0.1", Port: 8080, Password: "secret"}
}

func TestBuildRegistry_VMRunCmdClientForHostUsesSOCKS5(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	socksPort := ln.Addr().(*net.TCPAddr).Port
	var dials atomic.Int64
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			dials.Add(1)
			_ = c.Close()
		}
	}()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	destPort, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}

	vm, err := structpb.NewStruct(map[string]any{"rca": map[string]any{"func_path": "vm_run_cmd"}})
	if err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	spec := netx.Spec{ID: "lab", Type: netx.TypeSOCKS5, Host: "127.0.0.1", Port: socksPort}
	if spec.NoProxy != nil {
		t.Fatal("NoProxy must be empty so 127.0.0.1 is not bypassed")
	}
	if _, err := BuildRegistry([]*biz.ToolMeta{{Name: "rca-vm", Type: biz.ToolTypeRCA, Config: vm}}, nil, reg, RegistryBuildOptions{
		AgentProxyID: "lab",
		Proxies:      map[string]netx.Spec{"lab": spec},
	}); err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	tl, ok := reg.Get("vm_run_cmd")
	if !ok {
		t.Fatal("vm_run_cmd not registered")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = tl.Execute(ctx, map[string]any{
		"cmd":         "Get-Process",
		"host":        u.Hostname(),
		"port":        destPort,
		"timeout_sec": 2,
	})
	if n := dials.Load(); n == 0 {
		t.Fatal("ClientForHost must dial via netx SOCKS5 (Dial count=0)")
	}
}

func netxSpecSOCKS5(id string) netx.Spec {
	return netx.Spec{ID: id, Type: netx.TypeSOCKS5, Host: "127.0.0.1", Port: 1080}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// catalogOnlyProxyRepo implements biz.ProxyRepo with GetByID only.
// Any other method counts as an ACL/extra lookup and fails the test contract.
type catalogOnlyProxyRepo struct {
	byID      map[string]*biz.ProxyMeta
	aclCalled bool
}

func (r *catalogOnlyProxyRepo) markACL() {
	r.aclCalled = true
}

func (r *catalogOnlyProxyRepo) Create(context.Context, *biz.ProxyMeta) (*biz.ProxyMeta, error) {
	r.markACL()
	panic("Create must not be called")
}

func (r *catalogOnlyProxyRepo) GetByID(_ context.Context, id string) (*biz.ProxyMeta, error) {
	if r.byID == nil {
		return nil, biz.ErrProxyNotFound
	}
	m, ok := r.byID[id]
	if !ok {
		return nil, biz.ErrProxyNotFound
	}
	cp := *m
	return &cp, nil
}

func (r *catalogOnlyProxyRepo) List(context.Context, biz.ListOptions) ([]*biz.ProxyMeta, int, error) {
	r.markACL()
	panic("List must not be called")
}

func (r *catalogOnlyProxyRepo) Update(context.Context, *biz.ProxyMeta) (*biz.ProxyMeta, error) {
	r.markACL()
	panic("Update must not be called")
}

func (r *catalogOnlyProxyRepo) Delete(context.Context, string) error {
	r.markACL()
	panic("Delete must not be called")
}

func (r *catalogOnlyProxyRepo) ListReferences(context.Context, string, int) ([]biz.ProxyReference, bool, error) {
	r.markACL()
	panic("ListReferences must not be called")
}
