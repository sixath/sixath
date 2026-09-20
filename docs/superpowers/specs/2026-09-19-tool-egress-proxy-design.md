# 工具出网代理（命名代理资源）

> 状态：待评审  
> 日期：2026-09-19  
> 关联：Portal MCP 服务资源模型、`framework/tool/http_tool.go`、`framework/datasource`、`portal/internal/chat/agent_builder.go`  
> 触发：Agent 运行时节点到不了部分目标网段；部分工具（HTTP API）与部分数据源（ES / MySQL / Jaeger）需要走不同出口。

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 问题分类 | 工具在运行时进程内直连；节点路由达不到的目标，工具也达不到 |
| 原则 | **代理是运营资源，不是模型参数**。出站 Dial/Transport 由运行时按绑定解析，模型不可见、不可改 |
| 资源 | Portal 独立「代理」实体（对齐 MCP 服务：CRUD + `resources` ACL），类型 `http` 或 `socks5` |
| 绑定 | Agent 选一个默认代理（可空=直连）；每个工具/数据源：继承 / 直连 / 绑定另一代理 ID |
| 例外 | 代理上的 `no_proxy`（域名/后缀/CIDR）；工具显式绑定某代理时**强制走该隧道**（忽略 `no_proxy`） |
| 一期客户端 | `http_request`、web 检索、ES HTTP、MCP HTTP、Jaeger；MySQL / Mongo 仅 SOCKS5 Dial |
| 非目标 | 模型流量、SSH、stdio MCP 走代理；HTTP 正向代理硬连 MySQL；用代理绕过 SSRF；按节点自动选代理 |

一句话：运营维护几条命名隧道；Agent 选默认出口；个别工具覆盖；运行时统一 Dial，失败不得装成业务空结果。

## 1. 背景

现网出站都在 **Agent 运行时进程**（不是 Portal）：

- `http_request`、`web_*`、Tavily/Bocha、ES `ESHTTP`、MCP HTTP、模型客户端大多 `http.Client{Timeout: …}`，未设 `Transport.Proxy` / 自定义 `DialContext`。
- 自建 Client **不会**稳定继承进程 `HTTP_PROXY` / `ALL_PROXY`。
- MySQL 走驱动 Dial，与 HTTP 正向代理协议不兼容。
- Portal 已有「命名资源 + Agent 绑定」：工具、`mcp_servers`、`resources` 的 `view`/`use`/`edit`。代理应复用这套，而不是在每个工具 DSN 里粘贴 URL。

节点过不去的是**目标网段**，不是某一个工具类型；同一 Agent 上又会混 HTTP 与数据库，所以要同时支持 HTTP CONNECT 代理与 SOCKS5，且允许单个工具覆盖 Agent 默认。

## 2. 目标与非目标

### 目标（一期）

1. Portal 可创建/列出/更新/删除**命名代理**；测连通；密码回显打码。
2. Agent 可选择默认代理或直连；选用时调用方对该代理有 `use`。
3. 工具/数据源可：`inherit`（默认）/ `off`（直连）/ `proxy`（指定 `proxy_id`，覆盖 Agent）。
4. 运行时解析绑定后，HTTP 类客户端与 MySQL / Mongo SOCKS5 Dial 走同一套 ProxySpec；连接失败分类见 §6。
5. `no_proxy` 命中则直连；工具 `egress=proxy` 时忽略 `no_proxy`。
6. 模型工具 schema **不得**出现代理 URL/账号。SSRF 仍校验**最终目标**主机；代理主机允许为内网。

### 非目标（一期不做）

- 让模型在 `http_request` 参数里带 proxy。
- 用 HTTP 正向代理连接 MySQL（保存或装配时报错，不注册该查询工具）。
- SSH、stdio MCP、**调模型**的 HTTP 走工具/Agent 代理（模型出口仍用节点直连或进程环境变量，避免 LLM 流量误入办公网隧道）。
- 按运行时节点自动匹配代理、全局环境变量管理 UI、把代理写进 Skill YAML。
- 独立密钥托管（KMS）；一期密码列存库，对齐 MCP `env_json` 的务实程度，GET 不回传明文。
- 删除时代理引用自动改直连（有引用则 409）。

### 诚实上限

- 测连通只证明「到代理的握手」，不证明目标业务端口一定通。
- `no_proxy` 是启发式匹配（§4.3），不是完整 PAC。
- HTTP 代理只能代理 HTTP/HTTPS；非 HTTP 必须 SOCKS5。
- 同一 Portal Agent 在不同运行时节点上共用同一 `proxy_id`：若某节点到不了该代理地址，该 Agent 在该节点上会 transient 失败，一期不按节点分流。

## 3. 架构

```text
Portal
  proxies 表 + resources(type=proxy)
  Agent.proxy_id
  ToolConfig.egress_mode + proxy_id

绑定写入 (Create/Update Agent、保存工具 egress)
  编辑者必须对所选 proxy_id 有 use

会话装配 (agent_builder)
  按已绑定 id 加载明文 Spec（不按聊天用户重验 use）
  每个出站工具带 Egress{Mode, Spec}

framework/netx（名称可在实现时微调，必须单模块）
  HTTPClient(spec) / DialContext(spec)
  MatchNoProxy(host, no_proxy)

http_request / web / ES / MCP HTTP / Jaeger / MySQL / Mongo
  只用 netx，禁止各写一份 ProxyURL
```

数据流：运营改代理地址 → 下次会话装配生效（不必热更新长连接池一期）。MCP HTTP 客户端在装配时创建，跟现有 MCP 生命周期一致。

## 4. 资源与绑定

### 4.1 代理实体

对齐 `mcp_servers`：用户 slug `id`（`^[a-z][a-z0-9_-]{0,35}$`），`resources.payload_ref`。

| 字段 | 约束 |
|------|------|
| `id` | 主键 slug |
| `name` | 必填 |
| `description` | 可选 |
| `type` | `http` \| `socks5` |
| `host` | 必填；禁止空、禁止嵌入凭证 |
| `port` | 必填 1–65535 |
| `user` / `password` | 可选；GET/List 返回 `has_password`，不返回 `password`；更新时省略或空字符串 = 保持原值。一期不支持单独清空密码（删重建） |
| `no_proxy` | 字符串列表（库内 JSON）；可空 |
| `created_at` / `updated_at` | 与 MCP 相同 |

`type=http` 表示 HTTP 正向代理（HTTPS 目标用 CONNECT）。不单独做 `https` 代理类型（需要 TLS-to-proxy 时二期再加）。

`ResourceTypeProxy = "proxy"`。创建时写入 `resources`，可见性默认私有、创建者为 owner，与 MCP 一致。

### 4.2 绑定字段

**Agent**（`CreateAgent` / `UpdateAgent` / `AgentReply`）：

- `proxy_id`：可选。空 = 该 Agent 默认直连。非空必须存在且**编辑者**对该 id 有 `use`。

**工具配置** `ToolConfig` 的 `egress_mode` / `proxy_id` **只存在于 Portal 工具行**：`type=datasource`、`type=mcp`（HTTP MCP 工具行）、RCA（`es_log_query` / `jaeger_trace`）。

- `egress_mode`：`inherit` \| `off` \| `proxy`；缺省 `inherit`。
- `proxy_id`：仅 `egress_mode=proxy` 时必填；保存时编辑者必须对该 id 有 `use`。
- 解析后的 Spec 为 `http` 且目标为 MySQL / Mongo → 校验失败，不保存（含 `inherit` 到 Agent 的 HTTP 代理）。

无 Portal 行的内置工具（`http_request`、默认 web_*）：**只能继承 Agent**，没有单独绑定 UI。

Agent 绑定的 **MCP 服务目录**（`mcp_server_ids`，非工具行）一期只继承 Agent 默认代理，**不在 `mcp_servers` 表上增加 `egress_mode`**。若某 HTTP MCP 需要覆盖，做成 Portal `type=mcp` 工具行再绑 `egress`。

Hive 一期保持直连；保存时若 `egress_mode=proxy` → **拒绝**（不要静默直连）。Mongo 与 MySQL 相同：只接受 SOCKS5。

### 4.3 解析顺序

对一次出站，目标主机 `host`：

1. 工具 `egress_mode=off` → 直连。
2. 工具 `egress_mode=proxy` → 使用该 `proxy_id` 的 Spec，**不**套 `no_proxy`。
3. 否则若 Agent `proxy_id` 非空 → 使用该 Spec；若 `MatchNoProxy(host, spec.NoProxy)` → 直连。
4. 否则直连。

`MatchNoProxy`（必须单测钉死）：

- 条目 trim；忽略空行。
- `*` 匹配全部。
- 精确主机（大小写不敏感）。
- 以 `.` 开头：后缀匹配（`.example.com` 匹配 `a.example.com`）。
- CIDR：解析目标 IP 后包含则匹配；主机名无法解析时该条不匹配（不因此报错，继续下一条；全部无法匹配则走代理）。
- 端口不参与匹配（`host:3306` 只看 host）。

### 4.4 删除与导出

- Delete 前查引用：任意 Agent.`proxy_id` 或 ToolConfig.`proxy_id` → **409**，body 列出引用 id（上限 20 条 + `truncated`）。
- 导出 Agent/工具带 `proxy_id` / `egress_mode`，不带密码。导入时 id 不存在则装配失败（permanent），不静默直连。

## 5. 运行时接线

### 5.1 `framework/netx`

对外最小 API（实现可拆文件，禁止在 `http_tool.go` 里复制一份）：

- `type Spec struct { ID, Type, Host string; Port int; User, Password string; NoProxy []string }`
- `func (s Spec) HTTPProxyURL() (*url.URL, error)` — 仅 `type=http`
- `func DialContext(spec Spec) (proxy.ContextDialer 或等价)` — `socks5` 用 golang.org/x/net/proxy；`http` 不提供通用 TCP Dial
- `func HTTPClient(spec Spec, timeout time.Duration) *http.Client`
  - `http`：`Transport.Proxy = http.ProxyURL`
  - `socks5`：`Transport.DialContext = socks5`，`Proxy` 为 nil
  - `spec` 零值：默认 Transport（直连）
- `func MatchNoProxy(host string, rules []string) bool`

依赖：允许增加 `golang.org/x/net`（若仓库尚未用于 SOCKS）。

### 5.2 接到现有客户端

| 调用方 | 行为 |
|--------|------|
| `RegisterHTTPTool` | 增加可选 `HTTPClient` 或 `Spec`；默认直连以保持单测 |
| web_* / Tavily / Bocha | 装配时传入同一 Agent 解析结果（继承 Agent；无工具级覆盖） |
| ES `ESHTTP` / Jaeger | 按该工具 egress 解析后的 Client |
| MCP HTTP transport | **工具行**按 egress 注入；仅 `mcp_server_ids` 绑定的服务继承 Agent 默认；stdio 忽略 egress |
| MySQL / Mongo 数据源 | 解析后的 Spec 为 socks5 时注入 Dialer（含 Agent inherit）；解析结果为 http → builder/保存错误；无 Spec → 直连 |
| Hive | 一期直连，不读 egress |

模型客户端（OpenAI/DashScope/Ollama）**不**读 Agent `proxy_id`。

### 5.3 装配时机

`portal/internal/chat/agent_builder.go`（及 YAML `registerRCATools` 同类路径）：

- 收集本会话用到的 `proxy_id`（Agent + 各工具），一次加载 Spec。
- **不**按聊天用户（企微/频道 peer）重验 `resources` `use`。门禁是：绑定写入时编辑者已有 `use`；会话路径与现网 MCP/工具一样走 Agent 已绑定配置（`ListByAgentForSession` 同类）。
- 缺 id / 记录已删 / MySQL 解析为 http → 该工具注册失败并记日志，**不**把错误当 empty hit。
- 不把 Spec 写入工具 Description。

YAML 单机入口：`config` 可加可选 `proxies:` 列表 + `proxy_id`，与 Portal 字段同形，便于无 Portal 的 framework 测试；Portal 路径以库为准。

## 6. 错误处理

| 情况 | 合同 |
|------|------|
| 代理 TCP/握手失败、超时 | 工具 `ok: false`，`error_code=transient`；文案含代理 **id/name**，不含密码 |
| 目标经代理返回 4xx/5xx | 与现网 HTTP 工具相同（那是目标 HTTP 状态，不是代理配置错误） |
| 未知 `proxy_id`、MySQL 解析为 http | 装配或保存 **permanent**；不注册该出站工具 |
| 编辑者对 `proxy_id` 无 `use` | **仅绑定写入**（Create/Update Agent、保存工具 egress）permanent；会话装配不再用聊天身份验 use |
| `MatchNoProxy` 后直连仍失败 | 普通网络错误，不提代理 |

日志：`proxy_id`、`type`、目标 host（已有 SSRF/URL 规则）、错误类；禁止 password。

测连通（Portal RPC，需 `edit` 或至少 `use`——与 MCP test_connection 对齐）：

- `http`：对代理做 CONNECT（探测主机用固定 `example.com:443` 即可；成功条件为代理返回 200 或 405 且 TCP 已建立——实现时钉一条，单测用 httptest）。
- `socks5`：完成握手即可，不必连业务端口。
- 失败返回 transient 信息给表单，不写审计密码。

## 7. Portal / Web

- API：`/api/v1/proxies` CRUD + `POST /api/v1/proxies/{id}/test`，proto 新包或并入独立 `proxy.v1`，风格抄 `mcp_server`。
- 权限：List 按 `view`；绑到 Agent/工具要 `use`；改删 `edit`。
- Web：侧栏入口（MCP 旁）、列表、表单（类型、host、port、user、password、no_proxy 多行）、测连通按钮。
- Agent 表单：默认代理下拉（直连 + 有 use 的代理）。
- ToolForm：出网三选一；MySQL 下拉过滤 `socks5`。
- `toolExportFormat` / `client.ts`：编解码 `egress_mode`、`proxy_id`、Agent `proxy_id`。

## 8. 测试合同

必须有的单测（规划时按此拆任务）：

1. 解析顺序：off / 工具 proxy / Agent+no_proxy / 直连。
2. `MatchNoProxy`：精确、后缀、CIDR、`*`、无法解析主机名。
3. `HTTPClient`：http 类型请求打到 fake 代理；socks5 类型 Dial 打到 fake（可用桩 Dialer）。
4. MySQL + http 代理：保存或 Register 失败。
5. 删除有引用 → 409。
6. GET 代理不含 password；空密码更新保持原值。
7. `http_request` 在 Agent `proxy_id` 下走注入 Client。
8. ACL：无 `use` 的编辑者不能把代理绑到 Agent/工具；会话装配单测证明聊天用户无需 `use` 也能加载已绑定 Spec。
9. SSRF：`ValidateOutboundURL` / `web_extract` 现有用例仍过（不要去找不存在的 `http_request` SSRF 测试）。最终 URL 仍校验；代理 host 允许内网。

不做一期 e2e 真代理集群。

## 9. 文件地图（规划用）

| 区域 | 职责 |
|------|------|
| `framework/netx`（新） | Spec、Client、Dial、no_proxy |
| `framework/tool/http_tool.go` 等出站工具 | 注入 Client |
| `framework/datasource` MySQL/ES | Dial/Client |
| `framework/config` | 可选 YAML `proxies` |
| `portal` proto + biz/data/service | 代理 CRUD、test、Agent/Tool 字段、引用检查 |
| `portal/internal/chat/agent_builder.go` | 解析并注入 |
| `web` 列表/表单/Agent/ToolForm | 运营 UI |

## 10. 明确不做（复核）

技能里写死办公网代理 URL；默认 `HTTP_PROXY` 覆盖模型通道；PAC 文件；透明网关；按目标自动选多代理负载。
