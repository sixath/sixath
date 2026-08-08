# MCP Server 一等资源 + 原生 stdio 设计

**日期**: 2026-08-08  
**状态**: 已确认（2026-08-08）  
**动机**: Portal「新建工具」仅支持 HTTP MCP Endpoint；Cursor 类配置（`npx` + `args` + `env`）无法直接接入。要用 Confluence DC MCP（`@atlassian-dc-mcp/confluence`）等包，需要原生 stdio 与可共享的 MCP 服务资源。

**关联**:
- 现有 HTTP MCP：`framework/tool/mcp.go`、`portal` Tool type=`mcp`
- Skills 按需 MCP：`framework/config.MCPServerEntry`、`tool/skillops`、`load_skill`
- 非本设计：KnowledgeProvider / Confluence wiki ingest（见 knowledge-write 非目标）

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 产品形态 | **方案 2**：一等公民 **McpServer** 资源；Agent 绑定 `server_ids` |
| 传输 | `http`（现有）+ **原生 stdio**（`command` / `args` / `env`） |
| 生命周期 | 装配时 `Acquire` → `ListTools`；**进程池**按 `server_id`+指纹复用；**idle TTL** 回收 |
| 发现 | 池 entry 缓存 schema；指纹不变可跳过重复 `ListTools` |
| 安全 | 命令**白名单** + 参数/env 约束；禁止 shell 拼接 |
| 密钥 | **明文落库** + **UI/API 掩码回显**；提交 `***` 保留原值；KMS 二期 |
| 旧 MCP Tool | **保留**为 legacy；新路径主推 McpServer |
| 验证目标 | 挂上 Confluence DC MCP 并在对话中调用 |

---

## 1. 目标与非目标

### 目标（一期）

1. Portal 可 CRUD **MCP 服务**（HTTP 或 stdio），并绑定到 Agent。
2. framework 支持 stdio：基于 `mark3labs/mcp-go` 的 `NewStdioMCPClient`，与现有 `mcpClient` 抽象统一。
3. 进程池复用，避免每轮对话重复 `npx` 冷启动（idle 内）。
4. 命令白名单；env 敏感键掩码。
5. 金路径：`npx -y @atlassian-dc-mcp/confluence` + `CONFLUENCE_HOST` / `CONFLUENCE_API_TOKEN`。

### 非目标（一期不做）

- Confluence → KnowledgeProvider / 本地 wiki ingest
- 密钥 KMS / 外部 secret store
- MCP 市场、预置包模板商店
- 强制迁移旧 type=`mcp` Tool
- HTTP MCP 进程/连接池化
- 把 stdio 做成 sidecar 桥接（已否决；本设计为进程内原生 stdio）

---

## 2. 架构与资源模型

```text
┌─────────────┐     agent_mcp_servers      ┌──────────────────┐
│    Agent    │ ─────────────────────────► │    McpServer     │
└─────────────┘   (server_ids, sort)       │ id, name,         │
                                           │ transport,        │
                                           │ http|stdio fields │
                                           │ backend, timeout  │
                                           └────────┬─────────┘
                                                    │
                         RegisterMcpTool / 进程池    ▼
                                           ┌──────────────────┐
                                           │ framework/tool   │
                                           │ McpConfig + Pool │
                                           │ → 动态工具集     │
                                           └──────────────────┘
```

### 2.1 McpServer 字段

| 字段 | 说明 |
|------|------|
| `id` / `name` / `description` | `id` 同时作为 framework MCP server id（对齐 Skill `mcp_servers`） |
| `transport` | `http` \| `stdio` |
| HTTP | `endpoint`, `backend`（`metoro` \| `mark3labs`） |
| stdio | `command`, `args[]`, `env{}`；**一期仅 `mark3labs` 客户端**（`backend` 可省略，非空则必须为 `mark3labs`；`metoro` 不支持 stdio） |
| `timeout_sec` | 调用超时 |
| 归属/ACL | 与 Tool 一样进入统一 `resources`（`kind=mcp_server`） |

`id` 为**用户指定的 slug**（如 `confluence`），不是仅系统 UUID；需满足现有资源 id 字符规则，并与 Skill frontmatter `mcp_servers` 字符串一致。

### 2.2 绑定

- 表 `agent_mcp_servers`：`agent_id`, `server_id`, `sort_order`（删除 CASCADE，对齐 `agent_tools`）。
- Chat 装配：加载 Agent 绑定的 McpServer → `tool.McpConfig` → `RegisterMcpTool`。
- Skill 路径：Portal 已登记的 McpServer 同时转为 `McpServerEntry` 列表，供 `load_skill` 按 id 匹配。

### 2.3 与旧 MCP Tool

- 保留 type=`mcp` + `agent_tools`；ToolForm 可提示「推荐改用 MCP 服务」。
- 同一 `id` 禁止 Tool 侧 MCP server id 与 McpServer 撞名（注册幂等键是 server id）。

---

## 3. 进程池与发现时机

### 3.1 `tool.McpProcessPool`

进程级单例（Portal 启动注入或 `sync.Once`）：

| 键 | 值 |
|----|-----|
| key | `server_id` |
| entry | 活着的 `mcpClient` + 配置指纹 + refcount + lastUsed + 可选缓存 `[]Tool` |

**指纹**：`hash(transport, endpoint|command+args+env, backend)`。同 id 指纹变更 → 关闭旧进程再起新的。

**API 语义**

1. `Acquire(cfg)`：命中且指纹一致 → refcount++、刷新 lastUsed；否则白名单校验 → spawn → Initialize → 入池。
2. `RegisterMcpTool`：`Acquire` →（缓存未命中则）`ListTools` → 注册到当前 Registry；Execute 闭包按 `server_id` 再 `Acquire`/`CallTool`。
3. `Release(server_id)`：refcount--；不立刻杀。
4. sweeper：`refcount==0 && now-lastUsed > idleTTL` → Close + 移出。
5. 判定进程已死 → 池内驱逐，下次 `Acquire` 重建。

**默认** `idleTTL = 5m`，可用 `SATH_MCP_STDIO_IDLE_TTL` 覆盖。  
**仅 stdio 入池**；HTTP 保持现有行为。

### 3.2 Chat / Skill 衔接

```text
BuildRegistry(agent)
  for each bound McpServer:
    RegisterMcpTool(reg, cfg)   // Acquire → ListTools → 动态工具
  ... turn ...
  Release 本次 Acquire 过的 server_id
  // 进程可留在池内供下一轮复用
```

`load_skill` 触发的注册走同一池。

### 3.3 失败语义

| 场景 | 行为 |
|------|------|
| 白名单拒绝 | 创建/更新或 spawn 前明确错误 |
| spawn / Initialize / ListTools 失败 | 该 server 工具不进 Registry；打 Warn（禁止「标记成功却 0 工具」的静默成功） |
| CallTool 超时 | 返回工具错误；不影响池内其它 server |
| CallTool 进程死 | 驱逐 → 重建一次 → 仍失败则返回错误 |

---

## 4. 安全白名单

**默认允许 command 基名**：`npx`、`node`、`npm`（Windows 另认 `npx.cmd` / `node.exe`）。  
扩展：`SATH_MCP_STDIO_ALLOW_CMDS`（逗号分隔）。

**硬规则**

- 不经 shell：`exec.Command(command, args...)` only。
- `args`：单项 ≤ 512 字符，总数 ≤ 32。
- 黑名单危险旗标（如 node/npm `-e` / `--eval`）。
- `env` 键：`^[A-Z][A-Z0-9_]*$`；禁止覆盖 `PATH`、`LD_PRELOAD`、`DYLD_*` 等。
- 创建/更新与 spawn 前双重校验。

---

## 5. API / 存储 / UI

### 5.1 API

| 方法 | 路径 |
|------|------|
| `POST/GET` | `/api/v1/mcp-servers` |
| `GET/PUT/DELETE` | `/api/v1/mcp-servers/{id}` |
| `POST` | `/api/v1/agents/{id}/mcp-servers` body `{ server_ids }` |
| `DELETE` | `/api/v1/agents/{id}/mcp-servers` |

`GET Agent` 增加 `mcp_server_ids[]`。  
**一期必做**：`POST /api/v1/mcp-servers/{id}/test` → Acquire + ListTools + Release，返回工具名列表（用于表单「测试连接」与 Confluence 验收）。

**掩码**：Get 时键名含 `TOKEN`/`SECRET`/`PASSWORD`/`KEY`（大小写不敏感）的值显示为 `***`；Update 若值仍为 `***` 则保留库中原值。

权限：对齐 Tool ACL（绑定需 Agent edit + Server use）。

### 5.2 存储

- `mcp_servers`：字段见 §2.1；`args`/`env` JSON。
- `agent_mcp_servers`：见 §2.2。
- `resources.kind = mcp_server`。
- 删除 Server：**CASCADE 解绑** Agent 关系。

### 5.3 Web

1. MCP 服务列表 / 表单：transport 切换；stdio 的 command、args（一行一个）、env 表；敏感值 password + 掩码。
2. Agent 详情：绑定 MCP 服务多选。
3. 旧 ToolForm MCP：保留 + 推荐迁移文案。

### 5.4 framework 配置对齐

扩展 `config.MCPServerEntry`、`tool.McpConfig`、`skillops.McpServerEntry` 为同一形状；YAML `skills.mcp_servers` 与 DB 一致；stdio 同样走池与白名单。

---

## 6. 错误处理与日志

| 层 | 失败 | 行为 |
|----|------|------|
| 创建/更新 | 白名单失败 | `400` + reason（如 `MCP_STDIO_CMD_DENIED`） |
| 测试连接 | spawn/List 失败 | 明确错误；日志脱敏 |
| 装配 | Register 失败 | 跳过该 server；Warn |
| 删除仍被绑定 | CASCADE 解绑 | 一期采用 |

日志可记 command/args；**env 值一律 redacted**。

---

## 7. 测试与验收

### 7.1 测试

**framework**：白名单单测；进程池复用/指纹替换/idle 回收；stdio ListTools+CallTool（fixture 脚本，避免依赖 `-e`）；schema 缓存跳过二次 List。

**portal**：CRUD + ACL；掩码与 `***` 保留；bind/unbind；装配注入；test 连接。

**web**：transport 字段显隐；掩码不误清空。

### 7.2 Confluence 金路径

1. 创建 `id=confluence`，stdio，`npx` + `["-y","@atlassian-dc-mcp/confluence"]` + Host/Token env。
2. 测试连接含 `confluence_searchContent` 等。
3. Agent 绑定后对话可调用。
4. idle 内两轮对话池命中（日志/pid）。
5. Get 掩码；再 Save 不丢 Token。
6. `command=bash` → 拒绝。

---

## 8. 改造面清单（实现指引）

| 区域 | 改动 |
|------|------|
| `framework/tool/mcp.go` | `McpConfig` 增 transport/command/args/env；stdio 客户端；接入池 |
| `framework/tool` 新文件 | `McpProcessPool`、stdio allowlist |
| `framework/config` + `skillops` | `MCPServerEntry` 字段对齐 |
| `portal` api/biz/data | McpServer CRUD、迁移、`agent_mcp_servers`、装配接线 |
| `portal/internal/chat/agent_builder.go` | 绑定 Server → `RegisterMcpTool`；Release |
| `web` | McpServer 列表/表单；AgentDetail 绑定；env 掩码 |

---

## 9. 开放二期

- env 加密 / 外部 secret 引用
- 预置 Server 模板（Confluence / Jira）
- 旧 MCP Tool → McpServer 迁移向导
- HTTP 连接复用
- 强制人审 / 更细命令参数策略（按包模板）
