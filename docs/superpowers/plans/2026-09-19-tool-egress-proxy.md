# 工具出网代理 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 运营可管理命名 HTTP/SOCKS5 代理；Agent 选默认出口，工具可覆盖或直连；运行时统一 Dial，模型看不见代理。

**Architecture:** `framework/netx` 负责 Spec、no_proxy、HTTP Client / SOCKS5 Dial。Portal 代理资源抄 MCP 手写 HTTP API + `resources` ACL。绑定只在写入时验编辑者 `use`；`BuildRegistry` 按已绑定 id 加载 Spec 并注入客户端。`NewRegistry` 仍直连注册 `http_request`；装配后 `Registry.SetHTTPClient` 覆盖 Transport（不能二次 `Register`，现网重名会报 `tool already registered`）。

**Tech Stack:** Go（`framework/netx`、`golang.org/x/net/proxy`）、Portal GORM + 手写 `/api/v1/proxies`、现有 `tool.proto` / `agent.proto` 加字段、Web 列表/表单。

**Spec:** [docs/superpowers/specs/2026-09-19-tool-egress-proxy-design.md](../specs/2026-09-19-tool-egress-proxy-design.md)

**钉死：** HTTP 代理测连通成功条件 = CONNECT `example.com:443` 返回 **200**。模型 / SSH / stdio MCP 不走代理。提交仅在用户明确要求时 commit。

---

## File map

| File | 职责 |
|------|------|
| `framework/netx/spec.go` | `Spec`、`TypeHTTP`/`TypeSOCKS5`、`HTTPProxyURL` |
| `framework/netx/noproxy.go` | `MatchNoProxy` |
| `framework/netx/resolve.go` | `Binding` + `Resolve`（off / 工具 proxy 强制 / Agent+no_proxy） |
| `framework/netx/http_client.go` | `HTTPClient`；http=`Transport.Proxy`；socks5=`DialContext` |
| `framework/netx/dial.go` | SOCKS5 `DialContext` |
| `framework/netx/*_test.go` | §8.1–8.3 |
| `framework/tool/tool.go` | `SetHTTPClient` / `HTTPClient()` |
| `framework/tool/http_tool.go` | Execute 使用 registry overlay Client，保留 5s dial / 超时 |
| `framework/tool/web_tools.go` | 已有 `HTTPClient`；Portal 装配传入 |
| `framework/tool/jaeger_tool.go` | 可选 `*http.Client` |
| `framework/tool/mcp.go` | HTTP MCP 注入 Client；stdio 忽略 |
| `framework/datasource/config.go` 旁 `datasource.go` | 可选 `HTTPClient` / `DialContext`（不入 JSON） |
| `framework/datasource/mysql.go` | SOCKS5 RegisterDialContext；http Spec 拒绝 |
| `framework/datasource/elasticsearch.go` | 使用 cfg.HTTPClient |
| `portal/migrations/016_proxies.sql` | `proxies` 表 + `agents.proxy_id` |
| `portal/internal/data/model/proxy.go` | GORM |
| `portal/internal/biz/proxy.go` | CRUD、test、引用 409、ACL |
| `portal/internal/server/proxy.go` | 手写 HTTP，抄 `mcp_server.go` |
| `portal/api/agent/v1/agent.proto` | `proxy_id` |
| `portal/api/tool/v1/tool.proto` | `egress_mode` + `proxy_id` |
| `portal/internal/chat/agent_builder.go` | 注入；不重验聊天用户 use |
| `portal/internal/chat/web_wiring.go` | web 工具带 Agent 解析后的 Client |
| `web/src/pages/ProxyList.tsx` `ProxyForm.tsx` | 运营 UI |
| `web/src/pages/AgentDetail.tsx` `ToolForm.tsx` | 绑定 |

---

### Task 1: `framework/netx` 解析与 no_proxy

**Files:**
- Create: `framework/netx/spec.go`, `noproxy.go`, `resolve.go`, `noproxy_test.go`, `resolve_test.go`

- [ ] **Step 1: 写失败测试** `framework/netx/noproxy_test.go`

```go
package netx

import "testing"

func TestMatchNoProxy(t *testing.T) {
	cases := []struct {
		host string
		rules []string
		want bool
	}{
		{"es.local", []string{"es.local"}, true},
		{"ES.LOCAL", []string{"es.local"}, true},
		{"a.example.com", []string{".example.com"}, true},
		{"example.com", []string{".example.com"}, false},
		{"8.8.8.8", []string{"8.8.8.0/24"}, true},
		{"unresolvable.invalid", []string{"10.0.0.0/8"}, false},
		{"any.host", []string{"*"}, true},
		{"keep.proxy", []string{"  ", "other"}, false},
	}
	for _, c := range cases {
		if got := MatchNoProxy(c.host, c.rules); got != c.want {
			t.Fatalf("host=%s rules=%v got=%v want=%v", c.host, c.rules, got, c.want)
		}
	}
}
```

```go
func TestResolve_Order(t *testing.T) {
	agent := Spec{ID: "office", Type: TypeHTTP, Host: "127.0.0.1", Port: 8080, NoProxy: []string{"es.local"}}
	cat := map[string]Spec{"office": agent, "lab": {ID: "lab", Type: TypeSOCKS5, Host: "10.1.1.1", Port: 1080}}

	// off
	got, err := Resolve(Binding{Mode: ModeOff}, &agent, cat, "es.local")
	if err != nil || got != nil { t.Fatalf("off: %+v %v", got, err) }

	// tool proxy ignores no_proxy
	got, err = Resolve(Binding{Mode: ModeProxy, ProxyID: "office"}, &agent, cat, "es.local")
	if err != nil || got == nil || !got.Force { t.Fatalf("force: %+v %v", got, err) }

	// inherit + no_proxy → nil (direct)
	got, err = Resolve(Binding{Mode: ModeInherit}, &agent, cat, "es.local")
	if err != nil || got != nil { t.Fatalf("noproxy inherit: %+v %v", got, err) }

	// inherit miss no_proxy
	got, err = Resolve(Binding{Mode: ModeInherit}, &agent, cat, "gitlab.corp")
	if err != nil || got == nil || got.Spec.ID != "office" || got.Force { t.Fatalf("inherit: %+v", got) }

	// missing id
	_, err = Resolve(Binding{Mode: ModeProxy, ProxyID: "nope"}, &agent, cat, "x")
	if err == nil { t.Fatal("want missing proxy error") }
}
```

- [ ] **Step 2:** `cd framework && go test ./netx/ -count=1` → FAIL（包不存在）

- [ ] **Step 3: 最小实现**

```go
package netx

const (
	TypeHTTP   = "http"
	TypeSOCKS5 = "socks5"
	ModeInherit = "inherit"
	ModeOff     = "off"
	ModeProxy   = "proxy"
)

type Spec struct {
	ID, Type, Host, User, Password string
	Port int
	NoProxy []string
}

type Binding struct {
	Mode, ProxyID string
}

// Effective is the outbound tunnel. Nil Spec pointer from Resolve means direct.
type Effective struct {
	Spec  Spec
	Force bool // tool ModeProxy: do not apply NoProxy
}

func NormalizeMode(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "", ModeInherit:
		return ModeInherit
	case ModeOff, "direct":
		return ModeOff
	case ModeProxy:
		return ModeProxy
	default:
		return strings.ToLower(strings.TrimSpace(m))
	}
}

func Resolve(b Binding, agent *Spec, catalog map[string]Spec, destHost string) (*Effective, error) { /* spec §4.3 */ }

func MatchNoProxy(host string, rules []string) bool { /* spec §4.3；CIDR 用 net.ParseCIDR + LookupIP，失败跳过该条 */ }
```

未知 `Mode` 返回 error。`ModeProxy` 且 catalog 无 id → error。`agent==nil` 且 inherit → 直连 nil。

- [ ] **Step 4:** `go test ./netx/ -count=1` → PASS

---

### Task 2: HTTP Client 与 SOCKS5 Dial

**Files:**
- Create: `framework/netx/http_client.go`, `dial.go`, `http_client_test.go`
- Modify: `framework/go.mod` — `golang.org/x/net` 改为 direct（已是 indirect）

- [ ] **Step 1: 失败测试** — httptest 当「代理」：记录是否收到 CONNECT 或绝对 URL；客户端请求 `https://example.com/` 不得直连 example。

更简单、稳定的合同：

```go
func TestHTTPClient_HTTPProxySendsToProxy(t *testing.T) {
	saw := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw = true
		if r.Method != http.MethodConnect && r.URL.Host == "" && r.URL.Scheme == "" {
			t.Errorf("proxy got unexpected %s %s", r.Method, r.URL)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	port, _ := strconv.Atoi(u.Port())
	c := HTTPClient(Spec{Type: TypeHTTP, Host: u.Hostname(), Port: port}, 5*time.Second)
	req, _ := http.NewRequest(http.MethodGet, "http://target.example/path", nil)
	_, _ = c.Do(req)
	if !saw { t.Fatal("request did not hit proxy") }
}

func TestHTTPClient_ZeroSpecDirect(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := HTTPClient(Spec{}, 5*time.Second)
	resp, err := c.Get(srv.URL)
	if err != nil { t.Fatal(err) }
	resp.Body.Close()
	if !hit { t.Fatal("direct miss") }
}

func TestDialContext_SOCKS5UsesDialer(t *testing.T) {
	// 注入可替换的 socks5 工厂太重：测 HTTPClient socks 分支设置了 Transport.DialContext 非 nil 且 Proxy 为 nil。
	c := HTTPClient(Spec{Type: TypeSOCKS5, Host: "127.0.0.1", Port: 1080}, time.Second)
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.DialContext == nil { t.Fatal("want socks dial") }
	if tr.Proxy != nil {
		// Proxy 必须是 nil 或始终返回 nil 的函数
		u, _ := tr.Proxy(&http.Request{URL: &url.URL{Scheme: "http", Host: "x"}})
		if u != nil { t.Fatal("socks must not set HTTP proxy") }
	}
}

func TestHTTPClient_HonorNoProxy(t *testing.T) {
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer direct.Close()
	proxyHit := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHit = true
		w.WriteHeader(200)
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	port, _ := strconv.Atoi(pu.Port())
	du, _ := url.Parse(direct.URL)
	c := HTTPClient(Spec{Type: TypeHTTP, Host: pu.Hostname(), Port: port, NoProxy: []string{du.Hostname()}}, 5*time.Second)
	resp, err := c.Get(direct.URL)
	if err != nil { t.Fatal(err) }
	resp.Body.Close()
	if proxyHit { t.Fatal("no_proxy must skip proxy") }
}
```

`Force` 路径：`HTTPClient` 增加可选 `opts` 或 `HTTPClientEffective(Effective)`：`Force==true` 时清空 NoProxy 再建 Client。

- [ ] **Step 2:** 测试 FAIL

- [ ] **Step 3: 实现**

- `HTTPClient(spec Spec, timeout)`：零值 Spec → `http.Client{Timeout, Transport: clone DefaultTransport + 5s Dial}`（与现网 `http_tool` 一致）。
- `type=http`：`Transport.Proxy = func(req) { if !force && MatchNoProxy(req.URL.Hostname(), spec.NoProxy) { return nil, nil }; return spec.HTTPProxyURL() }`。
- `type=socks5`：`golang.org/x/net/proxy.SOCKS5`；有 user 则 `&proxy.Auth{}`；`DialContext` 包一层；`Proxy=nil`。
- 非法 type → panic 禁止，返回 `http.Client` 直连并在调用方先校验 type（校验放 `Spec.Validate()`）。

- [ ] **Step 4:** `go test ./netx/ -count=1` → PASS

---

### Task 3: 注入 `http_request` / web / Jaeger（不改 NewRegistry 默认）

**Files:**
- Modify: `framework/tool/tool.go` — `httpClient *http.Client` + `SetHTTPClient` / `HTTPClient()`
- Modify: `framework/tool/http_tool.go` — Execute 闭包读 `reg.HTTPClient()`；nil 则保持现有 Clone+5s Dial
- Modify: `framework/tool/http_tool_test.go` — 新测：SetHTTPClient 后请求打到 httptest 代理
- Modify: `framework/tool/jaeger_tool.go` — 保持 `RegisterJaegerTool(reg, queryURL string)`，增加可选 `...JaegerOption` 或 `client *http.Client` 用 **variadic 可选**，避免改所有调用点：`func RegisterJaegerTool(reg *Registry, queryURL string, client ...*http.Client)`。`nil`/省略 = 现网。
- 现有 `jaeger_tool_test.go` / `rca_builder.go` / `templates/rca_wiring.go` **不必改签名**（省略 client）。
- Modify: `framework/tool/web_tools.go` — `RegisterWebTools` 把 `cfg.HTTPClient` **同时**传给 `web_extract` **和** 搜索后端：`NewWebSearchBackend` / `BochaConfig.HTTPClient` / `TavilyConfig.HTTPClient`。现网只给了 extract，搜索仍直连。
- Modify: `framework/tool/web/bocha.go` `tavily.go` — 构造函数已有 HTTPClient 字段则接线；`NewWebSearchBackend(..., client *http.Client)` 或给 WebToolsConfig 已有 client 时重建 backend。
- Test: `web_tools_test.go` — Set 搜索 backend 的 client 被调用（可用 roundtrip 桩）。

**禁止：** `Register` 第二次 `http_request`。

- [ ] **Step 1:** `TestHTTPRequest_UsesRegistryHTTPClient`：httptest 代理 + `reg.SetHTTPClient(netx.HTTPClient(...))` + 调 `http_request` GET 某 URL，代理 `saw==true`。

- [ ] **Step 2:** FAIL（无 SetHTTPClient）

- [ ] **Step 3:** `http_tool.go` 在构造 `client` 前：

```go
if overlay := reg.HTTPClient(); overlay != nil {
    client = cloneHTTPClient(overlay, timeout)
}
```

`Do` 失败时若 overlay 来自代理，用 `netx.AnnotateError` 带上 proxy id（Spec 可挂在 context 或 registry 旁的 `HTTPClientSpec`）。更简单：`Registry` 同时存 `SetHTTPClient(c, spec)`，error 包装用 spec.ID。

共享 Client 不要在请求间改 Timeout：只改 clone 后的 `Client.Timeout`。

- [ ] **Step 4:** `go test ./tool/ -count=1 -run 'HTTPRequest|Jaeger'` → PASS。Jaeger 省略 client 的旧调用必须仍编译。

- [ ] **Step 5:** SSRF：`go test ./tool/ -count=1 -run 'WebExtract|OutboundURL|SSRF'`（以仓库实际测试名为准）。不要编造 `http_request` SSRF 测试。

---

### Task 4: ES / MySQL / MCP HTTP

**Files:**
- Modify: `framework/datasource/datasource.go` — 非 JSON 字段：

```go
HTTPClient  *http.Client `json:"-" yaml:"-"`
DialContext func(ctx context.Context, network, addr string) (net.Conn, error) `json:"-" yaml:"-"`
```

- Modify: `framework/datasource/elasticsearch.go` — `NewElasticsearch*` 把 `cfg.HTTPClient` 赋给 `ESHTTP.Client`
- Modify: `framework/datasource/mysql.go` — 若 `DialContext != nil`：`mysqldriver.RegisterDialContext(netName, ...)` 且 DSN `Net=netName`；netName = `"sixath-proxy-"+cfg.ID`（id 已规范化）。**禁止**全局覆盖 `"tcp"`。
- Modify: `framework/tool/mcp.go` — `McpConfig.HTTPClient *http.Client`；stdio **忽略**。
  - HTTP **默认 backend 是 metoro**（空 backend → `newMetoroClient`）。必须给 metoro `HTTPClientTransport` 注入 Client（库方法是 `WithClient`，实现时对着 `mcp-golang/transport/http` 核对）。
  - `mark3labs`：`NewStreamableHTTP` 选项是 **`WithHTTPBasicClient`**（不是 `WithHTTPClient`）。
  - **禁止**改 `http.DefaultClient`。
- 代理失败：HTTP/Dial error 包装为含 `proxy id/name` 的 transient（`http_request` / MySQL Dial）；**不得**含 password。可在 `netx` 提供 `AnnotateError(spec, err)`。
- Test: `framework/datasource/mysql_proxy_test.go` — `DialContext` 被调用（桩返回 error 即可证明走了自定义 Dial）；`HTTPClient` 非空且 type 校验在上层。
- Test: ES 已有 fake HTTP 则补「Client 非默认」或跳过若难测。

MySQL + HTTP 代理拒绝在 **Portal 保存 / BuildRegistry**，不在 `NewMySQLDataSource` 里猜。`NewMySQLDataSource` 只认 `DialContext`。

- [ ] **Step 1–4:** 先红后绿；`go test ./datasource/ ./tool/ -count=1`

---

### Task 5: Portal 代理 CRUD + 测连通 + 引用 409

**Files:**
- Create: `portal/migrations/016_proxies.sql`
- Create: `portal/internal/data/model/proxy.go`
- Create: `portal/internal/biz/proxy.go`, `proxy_test.go`, `proxy_test_connection.go`
- Create: `portal/internal/data/proxy_mysql.go`
- Create: `portal/internal/service/proxy.go`
- Create: `portal/internal/server/proxy.go`
- Modify: `portal/internal/biz/resource.go` — `ResourceTypeProxy = "proxy"`
- Modify: `portal/internal/data/data.go` AutoMigrate `&model.Proxy{}`
- Modify: `portal/internal/data/data.go` `ProviderSet` — **必须** `NewProxyRepo`
- Modify: `portal/internal/biz/biz.go` ProviderSet — `NewProxyUsecase`
- Modify: `portal/cmd/backend/wire.go` 的 `wire.Build` **显式**加入 `service.NewProxyService`（现网是逐个列 service，**没有** `service.ProviderSet`）。改完 `wire ./cmd/backend`。
- Modify: `portal/internal/server/http.go` — 注册路由；`NewHTTPServer` 加 `*service.ProxyService`
- Copy ACL 测试模式：`portal/internal/biz/mcp_server_test.go`

**表：**

```sql
CREATE TABLE IF NOT EXISTS proxies (
    id          VARCHAR(36) NOT NULL PRIMARY KEY,
    name        VARCHAR(128) NOT NULL,
    description TEXT NOT NULL,
    type        VARCHAR(16) NOT NULL,
    host        VARCHAR(256) NOT NULL,
    port        INT NOT NULL,
    user        VARCHAR(128) NOT NULL DEFAULT '',
    password    VARCHAR(256) NOT NULL DEFAULT '',
    no_proxy    JSON NULL,
    created_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    INDEX idx_proxies_name (name)
);
ALTER TABLE agents ADD COLUMN proxy_id VARCHAR(36) NULL;
```

slug：与 MCP 相同 `^[a-z][a-z0-9_-]{0,35}$`。

**API（手写，抄 MCP，不要新 proto 包）：**

- `POST/GET /api/v1/proxies` `GET/PUT/DELETE /api/v1/proxies/{id}` `POST /api/v1/proxies/{id}/test`
- JSON：`has_password`；永不回 `password`
- 更新：省略或 `password:""` → 保持；一期无清空
- Delete：查 `agents.proxy_id` 与 tools.config JSON（`egress`/`proxy_id`）→ 409 `{ "references": [...], "truncated": bool }` 最多 20

**测连通：**

- 权限：与 MCP test 相同（能 view 后需 `use`——对照 `TestConnection` 的 `requireMcpServerPerm(..., PermUse)`，代理抄同一档）
- `http`：对 `host:port` 发 `CONNECT example.com:443 HTTP/1.1`，**仅 status 200 成功**；407/5xx 失败
- `socks5`：TCP + 握手（无认证 `\x05\x01\x00` 期望 `\x05\x00`；有密码按 RFC1929）。不必 CONNECT 业务端口
- 错误 redact password

- [ ] **Step 1:** `TestCreateProxy_HidesPassword`、`TestUpdateProxy_EmptyPasswordKeeps`、`TestDeleteProxy_ConflictWhenAgentRefs`、`TestTestConnectionHTTP_200`（httptest 对 CONNECT 回 200）

- [ ] **Step 2–4:** 实现；`cd portal && go test ./internal/biz/ ./internal/service/ -count=1 -run Proxy`

---

### Task 6: Agent `proxy_id` + Tool `egress` 绑定写入

**Files:**
- Modify: `portal/api/agent/v1/agent.proto` — `CreateAgentRequest`/`UpdateAgentRequest`/`AgentReply` 增加 `string proxy_id = 15`（确认未占用号；`AgentReply` 现有 14 是 `mcp_server_ids`）
- Modify: `portal/api/tool/v1/tool.proto` — `ToolConfig`：`string egress_mode = 11;` `string proxy_id = 12;`（确认 11/12 空闲；当前到 10）
- Run: `cd portal && make api`（Windows 用 Git Bash 的 Makefile `api` target）
- Modify: `portal/internal/data/model/agent.go` — `ProxyID string`
- Modify: `portal/internal/biz` `AgentMeta` — `ProxyID`；`agent_mysql.go` `agentRowToMeta` / `Create` / `Update` 读写 `proxy_id` 列（只改 proto 不够）
- Modify: `portal/internal/service/agent.go` 编解码 `proxy_id`
- Modify: `portal/internal/service/tool.go` — `egress_mode`/`proxy_id` 进出 struct；`datasourceShouldEmit` 不必含 egress
- Modify: `portal/internal/biz/agent_usecase.go` Create/Update：非空 `proxy_id` → 存在 + 编辑者 `use`。**Create 必须把 `proxy_id` 写入行**（`AgentRepo.Create` 现为长参数列表，加字段后更新全部 fake；禁止只靠事后 Update）。
- Modify: `portal/internal/biz/tool.go` 保存前 `ValidateToolEgress`：
  - 非法 mode
  - `proxy` 无 id
  - 编辑者无 `use`
  - datasource mysql：解析 **保存时没有 Agent 上下文** → 只在 `egress_mode=proxy` 且该 proxy type=http 时拒绝；`inherit` 的 MySQL+Agent HTTP 在 **BuildRegistry** 拒绝
  - hive：`egress_mode=proxy` 拒绝；mongodb 与 mysql 相同，仅 SOCKS5
- Modify: `web/src/api/client.ts` `toolExportFormat.ts`
- Tests: `portal/internal/biz/agent_acl` 旁 `TestAgentUpdateProxyRequiresUse`；`tool_datasource_test` round-trip egress

- [ ] **Step 1–4:** proto 生成后编译；`go test ./internal/biz/ ./internal/service/ -count=1`

---

### Task 7: `BuildRegistry` 注入（会话不重验 use）

**Files:**
- Create: `portal/internal/chat/proxy_catalog.go` — `loadProxyCatalog(ctx, repo, agentProxyID, tools)` 一次加载 `map[string]netx.Spec`（**无 ACL**）
- Modify: `portal/internal/chat/agent_builder.go` — `RegistryBuildOptions` 增加 `AgentProxyID`、`Proxies map[string]netx.Spec`
- Modify: `portal/internal/service/chat.go` — **生产构造是 `ProvideChatServiceWithTurnTrace` → `NewChatServiceWithMemoryStore` / `newChatService`，不是测试用的 `NewChatService`。** 在 `newChatService` 增加 `proxyRepo`，并改 `ProvideChatServiceWithTurnTrace` 的 wire 入参。`wire.go` 现网逐个列 service。
- Modify: `portal/internal/service/agent.go` — `AgentService.Chat` 也是生产 `BuildRegistry`，同样注入 `ProxyRepo` 并传入 catalog。
- Modify: `portal/internal/chat/mcp_expand.go` — 热注册 `RegisterMcpTool` 带上 Agent inherit 的 `McpConfig.HTTPClient`，避免后装 HTTP MCP 直连。
- Modify: `portal/internal/chat/web_wiring.go` — `HTTPClient: reg.HTTPClient()`（`RegisterAgentRuntimeTools` 在 `BuildRegistry` **之后**调用，必须读 registry overlay，不要只在 agent_builder 里找 `registerWebTools`）
- Modify: `portal/internal/chat/es_log_clusters.go` — `ConfigFromMap` 之后给 ES `HTTPClient` 赋 Resolve 结果（`collectESLogClusters` / `registerESLogFromAgentTools` 实际创建 Client 的位置）
- Modify: `portal/internal/chat/rca_builder.go` — Jaeger 可选 client：`RegisterJaegerTool(reg, url, resolvedClient)`
- Modify: `portal/internal/chat/runtime_tools.go` — 若 web 注册不走 `web_wiring` 的 HTTPClient，在此确保 `RegisterWebTools` 看到 overlay

**装配注意（审阅补强，必须遵守）：**
- `mcp_expand`：给 `McpExpandOnMissOptions` 加 `HTTPClient`，并改 `chat.go`×2、`agent.go` 的 `NewMcpExpandOnMiss`，避免热注册传空 Client。
- MySQL/ES 的 Dial/Client 设在 `dsReg.Register` / `esReg.Register` **之前**的 `datasource.Config` 上（现网不直接调 `NewMySQLDataSource`）。
- Hive 即使 Agent 是 SOCKS5，inherit 也直连。Mongo 与 MySQL 相同：Agent SOCKS5 inherit 注入 DialContext。
- 目标主机已知的出站（ES/Jaeger/MySQL）调用 `Resolve(..., destHost)`，让 SOCKS5 的 `no_proxy` 生效。
- `NewChatService` / `NewChatServiceWithMemoryStore` 测试路径对 `proxyRepo` 传 nil 即可。

**调用方：** 三处生产 `BuildRegistry`（`chat.go`×2、`agent.go`×1）都走 `loadProxyCatalog`。测试可传空 map。

**流程：**
  1. `Resolve(http_request 的 inherit, agentSpec, cat, "")` 不能用空 dest 套 no_proxy——**http_request 在请求时由 Client 的 Proxy func 做 no_proxy**。装配时 inherit → `SetHTTPClient(HTTPClient(agentSpec))`（Client 自带 NoProxy）。
  2. 工具 `ModeOff`：该工具用直连 Client（Jaeger/ES/MCP 传 nil Client）。
  3. 工具 `ModeProxy`：`HTTPClientEffective(Force)`。
  4. MySQL inherit 到 HTTP Spec → **不注册**该数据源，log + binding.Err。
     MySQL 解析为 **socks5** → 给 `datasource.Config.DialContext = netx.DialContext(spec)` 再 `NewMySQLDataSource`（Task 4 只实现认字段，本步必须接线，否则 MySQL 永远直连）。
  5. 缺 proxy id：该出站工具跳过并记 permanent 日志。
  6. `mcp_server_ids` HTTP：一律 Agent inherit Client；stdio 忽略。
  7. Portal `type=mcp` 工具行：按该行 egress；**必须与 mcp_server 同 id 时走现有去重**（`HasMcpServer`）——计划实现：先注册工具行再注册 `ListByAgent` 服务，或反过来与现网一致。**现网是先 tools 循环再 servers 循环**；同 id 第二次 `RegisterMcpTool` 若会重复工具名则保持现网去重。覆盖 HTTP Client：**工具行先注册的客户端为准**，servers 循环遇到 `HasMcpServer` 则 skip（若现网不是 skip，改为 skip 以免直连客户端覆盖代理）。实现前读 `RegisterMcpTool` 去重；写测试 `TestBuildRegistry_MCPToolRowClientWins`。
  8. `registerESLogFromAgentTools`：egress 跟 **elasticsearch 数据源工具行**；RCA 内联 endpoint 跟 RCA 行。
  9. web_*：`BuildRegistry` 末尾 `SetHTTPClient`；随后 `RegisterAgentRuntimeTools` → `web_wiring` 读 `reg.HTTPClient()`。
  10. Jaeger：`RegisterJaegerTool(reg, url, client...)` 传入 Resolve 后的 Client。
  11. 出站失败：`netx.AnnotateError` 让模型看到代理 **id/name** + transient，不含密码。
- Test: `portal/internal/chat/agent_builder_test.go`
  - 聊天 ctx 无 proxy use 也能 Build（不调真实 ACL）
  - MySQL + agent HTTP proxy → 该 ds 不可用
  - MySQL + agent SOCKS5 → `DialContext` 非空（可用桩 Dial 记录调用）
  - http_request overlay 被 Set

- [ ] **Step 1–4:** `cd portal && go test ./internal/chat/ -count=1`

- [ ] **Step 5:** YAML：`framework/config.Config` 可选 `Proxies []netx.Spec` + `ProxyID`；`templates/rca_wiring.go` 有 ES/Jaeger 时注入。无 Portal 单测用 YAML。可放本任务末尾，小测一条。

---

### Task 8: Web 运营 UI

**Files:**
- Create: `web/src/pages/ProxyList.tsx`, `ProxyForm.tsx`（抄 `McpServerList/Form`：id、name、type、host、port、user、password、no_proxy 多行、测连通）
- Modify: `web/src/App.tsx` — nav「代理」+ routes `/proxies`
- Modify: `web/src/api/client.ts` — `proxyApi`
- Modify: `web/src/pages/AgentDetail.tsx` — 默认代理下拉（直连 + `proxyApi.list` 有权限的项）
- Modify: `web/src/pages/ToolForm.tsx` — 出网：继承 / 直连 / 指定代理；MySQL / Mongo 下拉只 `socks5`；Hive **不展示指定代理**（选了 inherit 即可；禁止选 proxy）
- 无浏览器工具时：至少 `npm test` 或现有 vitest；有则按用户规则点一遍列表→新建→Agent 下拉。

---

### Task 9: 回归

- [ ] `cd framework && go test ./netx/ ./tool/ ./datasource/ ./templates/ -count=1`
- [ ] `cd portal && go test ./internal/chat/ ./internal/service/ ./internal/biz/ -count=1`
- [ ] 现网：创建 socks5/http 代理、Agent 绑定、工具覆盖、删有引用 409、密码不回显。

---

## 明确不做

- 模型流量走代理；SSH；stdio MCP；PAC；KMS；删除改直连；`HTTP_PROXY` 环境变量产品化；二次 Register `http_request`。
