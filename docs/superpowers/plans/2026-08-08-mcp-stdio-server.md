# MCP Server + Native Stdio Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 MCP 提升为一等 `McpServer` 资源，支持原生 stdio（`command`/`args`/`env`）与进程池，使 Agent 能绑定并调用 `@atlassian-dc-mcp/confluence` 等 Cursor 同款 MCP。

**Architecture:** framework 扩展 `McpConfig` + stdio 白名单 + `McpProcessPool`（Acquire/Release/idle TTL/schema 缓存）；Portal 新增 `mcp_servers` / `agent_mcp_servers` CRUD 与 ACL；Chat `BuildRegistry` 注册绑定的 Server；Web 新列表/表单 + Agent 绑定。权威规格：[`2026-08-08-mcp-stdio-server-design.md`](../specs/2026-08-08-mcp-stdio-server-design.md)。

**Tech Stack:** Go（`framework/tool` + portal Kratos/GORM）、`mark3labs/mcp-go` stdio client、React（`web/src`）。

**Repos:** nested `framework/`、`portal/`、`web/`。**Do not commit unless asked.**

---

## File map

| Path | Responsibility |
|------|----------------|
| `framework/tool/mcp_stdio_allow.go` | command/args/env 白名单校验 |
| `framework/tool/mcp_stdio_allow_test.go` | 白名单单测 |
| `framework/tool/mcp_pool.go` | `McpProcessPool` Acquire/Release/sweeper/fingerprint/schema cache |
| `framework/tool/mcp_pool_test.go` | 池复用 / 指纹替换 / idle |
| `framework/tool/mcp.go` | `McpConfig` 扩字段；stdio 客户端；`RegisterMcpTool` 走池 |
| `framework/tool/mcp_stdio_test.go` | fixture 脚本 ListTools+CallTool |
| `framework/config/config.go` | `MCPServerEntry` 字段对齐 |
| `framework/tool/skillops/skill_tools.go` | `McpServerEntry` 字段对齐并传入完整 `McpConfig` |
| `portal/migrations/013_mcp_servers.sql` | `mcp_servers` + `agent_mcp_servers` |
| `portal/internal/data/model/mcp_server.go` | GORM models |
| `portal/internal/biz/resource.go` | `ResourceTypeMcpServer` |
| `portal/internal/biz/mcp_server.go` | usecase + mask env + validate |
| `portal/internal/data/mcp_server_mysql.go` | repo |
| `portal/api/mcp_server/v1/mcp_server.proto` | HTTP API |
| `portal/internal/service/mcp_server.go` | service |
| `portal/internal/server/http.go` | 挂路由 / wire |
| `portal/internal/chat/agent_builder.go` | 绑定 Server → Register；Release |
| `portal/internal/biz/agent*.go` / `data/agent_mysql.go` | `mcp_server_ids` bind/unbind |
| `web/src/api/client.ts` | `mcpServerApi` |
| `web/src/pages/McpServerList.tsx` / `McpServerForm.tsx` | UI |
| `web/src/pages/AgentDetail.tsx` | 绑定 MCP 服务 |
| `web/src/App.tsx` | 路由 + 导航 |
| `web/src/pages/ToolForm.tsx` | legacy MCP 提示文案 |

---

### Task 1: Stdio allowlist

**Files:**
- Create: `framework/tool/mcp_stdio_allow.go`
- Create: `framework/tool/mcp_stdio_allow_test.go`

- [ ] **Step 1: Write failing tests**

```go
package tool_test

func TestValidateStdioMcp_AllowsNpx(t *testing.T) {
	err := tool.ValidateStdioMcp("npx", []string{"-y", "@atlassian-dc-mcp/confluence"}, map[string]string{
		"CONFLUENCE_HOST": "confluence.example.com",
		"CONFLUENCE_API_TOKEN": "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateStdioMcp_DeniesBash(t *testing.T) {
	err := tool.ValidateStdioMcp("bash", []string{"-c", "id"}, nil)
	if err == nil {
		t.Fatal("expected deny")
	}
}

func TestValidateStdioMcp_DeniesNodeEval(t *testing.T) {
	err := tool.ValidateStdioMcp("node", []string{"-e", "console.log(1)"}, nil)
	if err == nil {
		t.Fatal("expected deny")
	}
}

func TestValidateStdioMcp_DeniesPathEnv(t *testing.T) {
	err := tool.ValidateStdioMcp("npx", []string{"-y", "x"}, map[string]string{"PATH": "/evil"})
	if err == nil {
		t.Fatal("expected deny")
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
cd framework && go test ./tool/ -run TestValidateStdioMcp -count=1
```

- [ ] **Step 3: Implement**

```go
package tool

// ValidateStdioMcp checks command basename against allowlist
// (npx|node|npm + SATH_MCP_STDIO_ALLOW_CMDS), args limits,
// dangerous flags (-e/--eval), env key regex and denylist (PATH, LD_PRELOAD, DYLD_*).
func ValidateStdioMcp(command string, args []string, env map[string]string) error
```

规则锁死见规格 §4。Windows：`npx.cmd` / `node.exe` 基名映射到 `npx` / `node`。

- [ ] **Step 4: Run — expect pass**

```bash
cd framework && go test ./tool/ -run TestValidateStdioMcp -count=1
```

---

### Task 2: Extend McpConfig + FromMap

**Files:**
- Modify: `framework/tool/mcp.go`（`McpConfig`、`McpConfigFromMap`、`NewMcpTool`）
- Create: `framework/tool/mcp_config_test.go`

- [ ] **Step 1: Failing tests for FromMap**

```go
func TestMcpConfigFromMap_Stdio(t *testing.T) {
	c := tool.McpConfigFromMap(map[string]any{
		"mcp": map[string]any{
			"id": "confluence",
			"transport": "stdio",
			"command": "npx",
			"args": []any{"-y", "@atlassian-dc-mcp/confluence"},
			"env": map[string]any{"CONFLUENCE_HOST": "h"},
			"backend": "mark3labs",
		},
	})
	if c == nil || c.Transport != "stdio" || c.Command != "npx" || len(c.Args) != 2 {
		t.Fatalf("%+v", c)
	}
}
```

- [ ] **Step 2: Extend struct**

```go
type McpConfig struct {
	Transport string            // ""|"http"|"stdio"；空且 Endpoint 非空 → http
	Endpoint  string
	Id        string
	Backend   string
	Command   string
	Args      []string
	Env       map[string]string
	TimeoutSec int
	mcpTool   *mcpTool
}
```

`McpConfigFromMap` 解析嵌套 `mcp` 与扁平字段。`NewMcpTool`：`transport==stdio` 时校验白名单且 backend 空→`mark3labs`，非 `mark3labs` 报错；HTTP 保持现状。

- [ ] **Step 3: Run tests**

```bash
cd framework && go test ./tool/ -run 'TestMcpConfigFromMap|TestValidateStdioMcp' -count=1
```

---

### Task 3: Stdio client + process pool

**Files:**
- Create: `framework/tool/mcp_pool.go`
- Create: `framework/tool/mcp_pool_test.go`
- Create: `framework/tool/testdata/mcp_stdio_fixture.js`（最小 JSON-RPC stdio MCP：list + echo tool）
- Modify: `framework/tool/mcp.go`（`newMark3labsStdioClient`、`RegisterMcpTool` 接池）

- [ ] **Step 1: Pool unit tests with fake client factory**

导出可测缝：`var stdioClientFactory = defaultStdioClientFactory`（测试里替换为记录 spawn 次数的 fake）。

```go
func TestMcpProcessPool_ReusesSameFingerprint(t *testing.T) {
	spawns := 0
	// inject factory that increments spawns
	p := tool.NewMcpProcessPool(tool.McpPoolOptions{IdleTTL: time.Hour})
	cfg := &tool.McpConfig{Id: "s1", Transport: "stdio", Command: "node", Args: []string{"x"}, Backend: "mark3labs"}
	_, release1, err := p.Acquire(context.Background(), cfg)
	// ...
	_, release2, err := p.Acquire(context.Background(), cfg)
	if spawns != 1 { t.Fatalf("spawns=%d", spawns) }
	release1(); release2()
}

func TestMcpProcessPool_FingerprintChangeRespawns(t *testing.T) { /* env 变更 → spawn==2 */ }

func TestMcpProcessPool_IdleEvicts(t *testing.T) {
	p := tool.NewMcpProcessPool(tool.McpPoolOptions{IdleTTL: 20 * time.Millisecond})
	// Acquire, Release, sleep > TTL, assert next Acquire respawns
}
```

- [ ] **Step 2: Implement pool**

```go
type McpPoolOptions struct {
	IdleTTL time.Duration // default 5m; override SATH_MCP_STDIO_IDLE_TTL
}

type McpProcessPool struct { /* sync.Mutex; map[string]*poolEntry; sweeper */ }

func DefaultMcpProcessPool() *McpProcessPool // sync.Once

// Acquire returns client + release func. HTTP configs may bypass pool
// (caller uses NewMcpTool directly) — RegisterMcpTool 对 stdio 用池，对 http 保持旧路径。
func (p *McpProcessPool) Acquire(ctx context.Context, cfg *McpConfig) (mcpClient, func(), error)

func Fingerprint(cfg *McpConfig) string // hash transport+endpoint|command+args+env+backend
```

entry 字段：`client`, `fingerprint`, `refcount`, `lastUsed`, `tools []Tool`（schema 缓存）。

- [ ] **Step 3: Wire RegisterMcpTool**

stdio：

1. `ValidateStdioMcp`
2. `DefaultMcpProcessPool().Acquire`
3. 若 entry 无 cached tools → `ListTools` 并缓存
4. 注册 tools；`Execute` 内再 `Acquire` → `CallTool` → `release`
5. `RegisterMcpTool` 返回或通过 out-param 记录需在 Registry 生命周期结束时调用的 `Release`（见 Task 7：`RegistryBuildResult` 增加 `McpReleases []func()`）

一期最小：`Acquire` 在 `RegisterMcpTool` 时 +1，注册完立刻 `Release`（refcount→0 但进程因 idle TTL 仍活着）；`CallTool` 时再 `Acquire`。这样不必改 Registry 析构也能复用进程。

- [ ] **Step 4: Integration test with fixture**

```bash
cd framework && go test ./tool/ -run 'TestMcpProcessPool|TestStdioMcp_ListAndCall' -count=1
```

`TestStdioMcp_ListAndCall`：`command=node`, `args=[testdata/mcp_stdio_fixture.js]`（`node` 在白名单内；**不要**用 `-e`）。

---

### Task 4: Align config + skillops entries

**Files:**
- Modify: `framework/config/config.go`
- Modify: `framework/config/config_test.go`（YAML 含 command/args/env）
- Modify: `framework/tool/skillops/skill_tools.go`

- [ ] **Step 1: Extend structs**

```go
// config.MCPServerEntry / skillops.McpServerEntry
type MCPServerEntry struct {
	ID        string            `json:"id" yaml:"id"`
	Transport string            `json:"transport" yaml:"transport"`
	Endpoint  string            `json:"endpoint" yaml:"endpoint"`
	Backend   string            `json:"backend" yaml:"backend"`
	Command   string            `json:"command" yaml:"command"`
	Args      []string          `json:"args" yaml:"args"`
	Env       map[string]string `json:"env" yaml:"env"`
}
```

- [ ] **Step 2: `registerSkillMcpFromMeta` 构建完整 `tool.McpConfig`（含 stdio 字段）**

- [ ] **Step 3: Run**

```bash
cd framework && go test ./config/ ./tool/skillops/ -count=1
```

---

### Task 5: Portal migration + models

**Files:**
- Create: `portal/migrations/013_mcp_servers.sql`
- Create: `portal/internal/data/model/mcp_server.go`
- Create: `portal/internal/data/model/agent_mcp_server.go`
- Modify: `portal/internal/biz/resource.go`（`ResourceTypeMcpServer = "mcp_server"`）

- [ ] **Step 1: SQL**

```sql
CREATE TABLE IF NOT EXISTS mcp_servers (
    id           VARCHAR(36)  NOT NULL PRIMARY KEY,
    name         VARCHAR(128) NOT NULL,
    description  TEXT         NOT NULL,
    transport    VARCHAR(16)  NOT NULL,
    endpoint     VARCHAR(512) NOT NULL DEFAULT '',
    backend      VARCHAR(32)  NOT NULL DEFAULT '',
    command      VARCHAR(256) NOT NULL DEFAULT '',
    args_json    JSON         NULL,
    env_json     JSON         NULL,
    timeout_sec  INT          NOT NULL DEFAULT 60,
    created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    INDEX idx_mcp_servers_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS agent_mcp_servers (
    agent_id    VARCHAR(36) NOT NULL,
    server_id   VARCHAR(36) NOT NULL,
    sort_order  INT         NOT NULL DEFAULT 0,
    created_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (agent_id, server_id),
    CONSTRAINT fk_agent_mcp_servers_agent FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    CONSTRAINT fk_agent_mcp_servers_server FOREIGN KEY (server_id) REFERENCES mcp_servers(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

`id` 规则：用户 slug，`^[a-z][a-z0-9_-]{0,35}$`（适配 `resources.payload_ref` size 36）。

- [ ] **Step 2: GORM models + ResourceType 常量**

- [ ] **Step 3: Apply migration on local DB**（按项目既有 migrate 方式）

---

### Task 6: Portal biz usecase（CRUD + mask + ACL）

**Files:**
- Create: `portal/internal/biz/mcp_server.go`
- Create: `portal/internal/biz/mcp_server_test.go`
- Create: `portal/internal/data/mcp_server_mysql.go`
- Wire: `portal/cmd` / `data` providers（对齐 Tool）

- [ ] **Step 1: Types**

```go
type McpServerMeta struct {
	ID, Name, Description, Transport, Endpoint, Backend, Command string
	Args []string
	Env map[string]string
	TimeoutSec int
	CreatedAt, UpdatedAt time.Time
}

func MaskSensitiveEnv(env map[string]string) map[string]string
func MergeEnvPreservingMasked(existing, incoming map[string]string) map[string]string
func ValidateMcpServerInput(m *McpServerMeta) error // transport 互斥 + tool.ValidateStdioMcp
```

掩码键：含 `TOKEN`/`SECRET`/`PASSWORD`/`KEY`（大小写不敏感）→ `***`。

- [ ] **Step 2: Usecase** 对齐 `ToolUsecase`：Create 写 `resources`（Type=`mcp_server`, PayloadRef=id）；List 用 `VisiblePayloadRefs`；Delete CASCADE 解绑（FK 已处理）。

- [ ] **Step 3: Tests** — mask/merge；Create 校验 `bash` 失败；ACL stranger 不可见。

```bash
cd portal && go test ./internal/biz/ -run McpServer -count=1
```

---

### Task 7: API + Agent bind + Chat 装配 + test 连接

**Files:**
- Create: `portal/api/mcp_server/v1/mcp_server.proto`（生成 pb）
- Create: `portal/internal/service/mcp_server.go`
- Modify: `portal/internal/server/http.go`（注册）
- Modify: `portal/api/agent/v1/agent.proto` + service（`mcp_server_ids`、Bind/Unbind）
- Modify: `portal/internal/data/agent_mysql.go`、`biz/agent_usecase.go`
- Modify: `portal/internal/chat/agent_builder.go` + chat 调用处
- Create: `portal/internal/service/mcp_server_test.go`（或 handler 单测）

- [ ] **Step 1: Proto 路由**

```
POST/GET    /api/v1/mcp-servers
GET/PUT/DELETE /api/v1/mcp-servers/{id}
POST        /api/v1/mcp-servers/{id}/test
POST        /api/v1/agents/{id}/mcp-servers   body { server_ids }
DELETE      /api/v1/agents/{id}/mcp-servers
```

`AgentReply.mcp_server_ids` repeated string。

- [ ] **Step 2: Test 连接 handler**

加载 Server → `tool.McpConfig` → `DefaultMcpProcessPool().Acquire` → `ListTools` → Release → 返回 `[]string` 工具名。失败脱敏。

- [ ] **Step 3: BuildRegistry 扩展**

```go
func BuildRegistry(tools []*biz.ToolMeta, servers []*biz.McpServerMeta, reg *tool.Registry) (*RegistryBuildResult, error) {
	// 现有 tools 循环...
	for _, s := range servers {
		mc := mcpServerToConfig(s)
		tool.RegisterMcpTool(reg, mc)
		mcpServers = append(mcpServers, skillops entry from mc)
	}
}
```

Chat 路径：`ListByAgent` tools **且** `ListMcpServersByAgent`。绑定权限：Agent edit + 每个 Server `PermUse`。

- [ ] **Step 4: 单测装配** — fake server meta stdio fixture → Registry 含 echo tool 名。

```bash
cd portal && go test ./internal/chat/ ./internal/service/ ./internal/biz/ -count=1
```

---

### Task 8: Web UI

**Files:**
- Modify: `web/src/api/client.ts`（`McpServer` 类型 + `mcpServerApi` + agent bind helpers）
- Create: `web/src/pages/McpServerList.tsx`
- Create: `web/src/pages/McpServerForm.tsx`
- Modify: `web/src/App.tsx`（nav + routes `/mcp-servers`）
- Modify: `web/src/pages/AgentDetail.tsx`（绑定 MCP 服务）
- Modify: `web/src/pages/ToolForm.tsx`（legacy 提示）

- [ ] **Step 1: API client**

```ts
export type McpServer = {
  id: string
  name: string
  description: string
  transport: 'http' | 'stdio'
  endpoint?: string
  backend?: string
  command?: string
  args?: string[]
  env?: Record<string, string>
  timeout_sec?: number
}
export const mcpServerApi = {
  list, get, create, update, remove, test,
}
// agentApi.bindMcpServers / unbindMcpServers
```

- [ ] **Step 2: Form**

- transport 切换显隐字段
- args：textarea 一行一个
- env：键值行；值 `type=password`；加载后敏感值为 `***`；提交时未改动的 `***` 原样回传（后端 merge）
- 「测试连接」按钮调 `/test`，展示工具名

- [ ] **Step 3: AgentDetail**

并列「绑定工具」增加「MCP 服务」多选（`mcpServerApi.list`），保存调 bind。

- [ ] **Step 4: ToolForm** 在 MCP 区块顶部：`推荐改用「MCP 服务」管理 stdio/HTTP 服务，本表单为 legacy。`

- [ ] **Step 5: 手动点检** — 创建 confluence 形配置（可用 fixture 替代真 Token）→ 测试连接 → 绑定 Agent。

---

### Task 9: Confluence 金路径验收

- [ ] **Step 1:** 真实或 staging Confluence：创建 Server `id=confluence`，stdio，`npx`/`-y`/`@atlassian-dc-mcp/confluence`，env Host+Token。

- [ ] **Step 2:** 测试连接含 `confluence_searchContent`（或包实际工具名）。

- [ ] **Step 3:** Agent 绑定；对话调用搜索。

- [ ] **Step 4:** 两轮对话内确认池命中（framework 打 `mcp_pool hit id=confluence` 日志）。

- [ ] **Step 5:** Get 掩码；再 Save 不丢 Token；`command=bash` 创建被拒。

- [ ] **Step 6:** 更新 `portal/docs` 或 README 小节（可选短文档：如何挂 Confluence MCP）。

---

## Spec coverage checklist

| Spec 项 | Task |
|---------|------|
| McpServer 一等资源 + agent 绑定 | 5–7 |
| 原生 stdio + mark3labs only | 2–3 |
| 进程池 + idle TTL + schema cache | 3 |
| 装配 ListTools / Call 复用 | 3, 7 |
| 命令白名单 | 1 |
| 明文 + 掩码 / `***` 保留 | 6–8 |
| test 连接 API | 7–8 |
| 旧 MCP Tool legacy | 8 |
| config/skillops 对齐 | 4 |
| Confluence 验收 | 9 |
| 非目标（Knowledge ingest / KMS） | 不实现 |

---

## Self-review notes

- 无 TBD；stdio backend 强制 mark3labs（Task 2）。
- Register 策略采用「Register 末 Release、Call 再 Acquire」以复用 idle 进程，避免强制 Registry 析构钩子；与规格 §3「Release 本次 Acquire」兼容。
- `resources.type` 长度：`mcp_server` ≤ 16；`id` slug ≤ 36。
