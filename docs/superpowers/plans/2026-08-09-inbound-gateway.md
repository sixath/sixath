# Inbound Gateway Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地独立 Gateway + Portal `/runtime/v1` 契约，使 Web 与通用 Webhook 统一入站对话；关闭对外直连 Chat 入站。

**Architecture:** Gateway 终止用户/Webhook 鉴权，调用 Portal Runtime（service token）；Webhook 用 `channel+peer` 映射续聊；Web 保持多 session UX，经 Gateway 代理会话 CRUD + SSE；Portal 既有 Channel/wecom 出站不动。权威规格：[`2026-08-09-inbound-gateway-design.md`](../specs/2026-08-09-inbound-gateway-design.md)。

**Tech Stack:** Go（`gateway/` 与 `portal/go.mod` 对齐，当前 `go 1.26`）、React/Vite（`web/` 代理切流）、Docker Compose、MySQL（peer 映射表）。

**Repos:** `gateway/` 落在 sixath 编排仓；`portal/`、`web/` 为嵌套仓——改动在对应目录提交。**Do not commit unless asked**（文档仓提交除外若用户已要求）。

**Locked decisions (from §9):**

| 项 | 锁定 |
|----|------|
| Web 代理切面 | Gateway 对外兼容 **Web 实际使用的会话路径**（见 Task 9 路径表，含 `/agents/{id}/sessions`、`messages/stream`、`search`、`rewind`）；其余 `/api`（Agent/Tool/Channel 管理等）仍直连 Portal |
| `peer_id`（Web） | 登录用户 ID，仅 ACL；不折叠多会话 |
| `peer_id`（Webhook） | 请求体字段；`channel+peer` → 唯一 session |
| Service token | 配置项 `runtime.service_token` / env `SATH_RUNTIME_TOKEN`；compose 与本地默认 `dev-runtime-token` |
| Web 登录态 | **不透明 Bearer**（与现网一致：SHA-256 → `UserIDByTokenHash`）。Gateway **不**自建 JWT；通过 Portal `GET /api/v1/auth/me`（Task 2b 新增）解析 `user_id`，再以 service token 调 Runtime |
| Turn 超时 | 默认 120s |
| final 形态 | Portal 同步 JSON；Gateway 再 POST `reply_url` |

---

## File map

| Path | Responsibility |
|------|----------------|
| `portal/internal/data/model/channel_peer_session.go` | `channel_peer_sessions` 表：channel_id+peer_id → session_id |
| `portal/internal/biz/channel_peer.go` | Repo 接口 + Resolve 领域逻辑 |
| `portal/internal/data/channel_peer_mysql.go` | GORM 实现 |
| `portal/internal/runtime/auth.go` | Service token + `X-Sath-User-Id` 校验中间件 |
| `portal/internal/server/auth_me.go` | `GET /api/v1/auth/me`（不透明 Bearer → user_id） |
| `portal/internal/runtime/service.go` | Resolve / CreateSession / List / Turns stream\|final |
| `portal/internal/runtime/http.go` | 注册 `/runtime/v1/*` |
| `portal/internal/server/http.go` | 挂 Runtime；Chat 公共入站按开关拒绝 |
| `portal/configs/*.yaml` | `runtime.service_token`、`chat.public_inbound_enabled: false` |
| `gateway/go.mod` | module `github.com/sixath/gateway` |
| `gateway/cmd/gateway/main.go` | 进程入口 |
| `gateway/internal/config/config.go` | Portal URL、token、listen、channels 文件 |
| `gateway/internal/channel/registry.go` | 渠道配置加载 |
| `gateway/internal/runtimeclient/client.go` | Portal Runtime HTTP 客户端 |
| `gateway/internal/session/router.go` | webhook resolve 短缓存（随 Task 8 使用） |
| `gateway/internal/adapter/adapter.go` | Adapter 接口 |
| `gateway/internal/adapter/web.go` | Web 鉴权 + `/api/v1/sessions*` → Runtime |
| `gateway/internal/adapter/webhook.go` | `/hooks/{id}`、202、async reply、幂等 |
| `gateway/internal/reply/dispatcher.go` | SSE 透传 / reply_url POST |
| `gateway/configs/config.example.yaml` | 示例渠道 + portal |
| `gateway/Dockerfile` | 镜像 |
| `docker-compose.yml` | gateway 服务；web 依赖 gateway |
| `web/vite.config.ts` | sessions 代理 → gateway；其余 `/api` → portal |
| `web` nginx（若有） | 生产同样切流 |
| `_neo4j_q/verify_inbound_gateway.ps1` | E2E 烟雾（可选但建议） |

---

### Task 1: Portal peer→session 映射存储

**Files:**
- Create: `portal/internal/data/model/channel_peer_session.go`
- Create: `portal/internal/biz/channel_peer.go`
- Create: `portal/internal/data/channel_peer_mysql.go`
- Create: `portal/internal/biz/channel_peer_test.go`（或 data 层测试用 sqlite/mysql）
- Modify: `portal/internal/data/data.go`（AutoMigrate 注册）
- Modify: `portal/internal/data/data.go` ProviderSet / `portal/internal/biz/biz.go` ProviderSet
- Modify: `portal/cmd/backend/wire.go` → 重新 `wire` 生成 `wire_gen.go`

- [ ] **Step 1: Write failing test for Resolve semantics**

```go
func TestChannelPeerResolve_SameKeySameSession(t *testing.T) {
	// fake repo or integration: Resolve(ch, peer, agentA) twice → same session_id, created false on 2nd
	// Resolve(ch, peer, agentB) → still same session_id, agent unchanged
}
func TestChannelPeerResolve_DifferentPeerDifferentSession(t *testing.T) {
	// peer1 vs peer2 → different session_id
}
```

- [ ] **Step 2: Run test — expect fail**

```bash
cd portal && go test ./internal/biz/ -run TestChannelPeerResolve -count=1
```

Expected: undefined.

- [ ] **Step 3: Implement model + repo + usecase**

```go
// model
type ChannelPeerSession struct {
	ChannelID string `gorm:"primaryKey;size:64"`
	PeerID    string `gorm:"primaryKey;size:128"`
	SessionID string `gorm:"size:36;not null;index"`
	AgentID   string `gorm:"size:36;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
```

Resolve：查映射 → 有则返回；无则 `ChatSessionRepo.Create(userID="", agentID, title)`（webhook 可用 peer 作 user 占位或专用前缀 `peer:{id}`）并插入映射。**已存在时忽略新 agent_id。**

- [ ] **Step 4: Tests pass + AutoMigrate**

```bash
cd portal && go test ./internal/biz/ -run TestChannelPeerResolve -count=1
```

- [ ] **Step 5: Commit (portal repo)**

```bash
cd portal && git add internal/data/model/channel_peer_session.go internal/biz/channel_peer.go internal/data/channel_peer_mysql.go internal/data/data.go internal/biz/biz.go cmd/backend/wire.go cmd/backend/wire_gen.go && git commit -m "feat(runtime): persist channel+peer session mapping"
```

---

### Task 2: Portal `auth/me` + Runtime auth 中间件

**Files:**
- Create: `portal/internal/server/auth_me.go`（或并入既有 auth handlers）
- Create: `portal/internal/runtime/auth.go`
- Create: `portal/internal/runtime/auth_test.go`
- Modify: `portal/internal/server/http.go` — `GET /api/v1/auth/me`
- Modify: configs — `runtime.service_token`

**Web 鉴权策略（锁定）：**

1. 浏览器继续带现有不透明 `Authorization: Bearer <session_token>`。
2. Gateway 先请求 Portal `GET /api/v1/auth/me`（用户 token）→ `{ user_id }`。
3. Gateway 再调 `/runtime/v1/*`，只带 `Authorization: Bearer <runtime_token>` + `X-Sath-User-Id: <user_id>`。
4. Portal Runtime **拒绝**用户 session token 作为 Runtime 信任根。

- [ ] **Step 1: Failing tests**

```go
func TestAuthMe_ReturnsUserID(t *testing.T) { /* valid opaque token → user_id */ }
func TestRuntimeAuth_RejectsMissingToken(t *testing.T) {}
func TestRuntimeAuth_AcceptsServiceTokenAndUserHeader(t *testing.T) {}
func TestRuntimeAuth_RejectsUserSessionTokenAlone(t *testing.T) {}
```

- [ ] **Step 2: Run — expect fail**

```bash
cd portal && go test ./internal/runtime/ ./internal/server/ -run "TestAuthMe|TestRuntimeAuth" -count=1
```

- [ ] **Step 3: Implement `auth/me` + Runtime middleware**

`/runtime/v1` 用自定义 Route 注册（同 `chat_sse`），每个 handler 显式套 Runtime auth；全局 `middleware.Auth` 对 `/runtime/v1` skip。

- [ ] **Step 4: Pass + commit**

```bash
cd portal && git commit -am "feat(auth): me endpoint and runtime service-token gate"
```

---

### Task 3: Portal Runtime HTTP — sessions CRUD + resolve + messages

**Files:**
- Create: `portal/internal/runtime/service.go`
- Create: `portal/internal/runtime/http.go`
- Create: `portal/internal/runtime/sessions_test.go`
- Modify: `portal/internal/server/http.go`（注册路由，走 Runtime auth）

**Runtime 最小路由（供 Task 9 代理，必须齐）：**

| Runtime | 用途 |
|---------|------|
| `POST /runtime/v1/sessions/resolve` | webhook peer 续聊 |
| `POST /runtime/v1/sessions` | Web 创建（body: agent_id, title?; user from header） |
| `GET /runtime/v1/sessions` | Web 全量列表 |
| `GET /runtime/v1/agents/{agent_id}/sessions` | Web 按 Agent 列表 |
| `GET /runtime/v1/sessions/{id}` | get |
| `PUT /runtime/v1/sessions/{id}` | update title |
| `DELETE /runtime/v1/sessions/{id}` | delete |
| `GET /runtime/v1/sessions/{id}/messages` | 历史 |
| `GET /runtime/v1/sessions/search` | 搜索（若 ChatUsecase 已有则封装） |
| `POST /runtime/v1/sessions/{id}/rewind` | rewind（若一期 Web 仍调用；否则 Gateway 可 501 并记入风险——**一期要求代理现有行为，必须封装**） |

- [ ] **Step 1: Contract tests（httptest）**

覆盖 resolve、create、list-by-agent、get、messages、无 token → 401、user mismatch → 403。

- [ ] **Step 2: Run — fail**

```bash
cd portal && go test ./internal/runtime/ -count=1
```

- [ ] **Step 3: Implement handlers**

委托现有 `ChatUsecase`。JSON 字段对齐现有 Chat HTTP 响应，降低 Gateway 转换量。

- [ ] **Step 4: Pass + commit**

```bash
cd portal && git commit -am "feat(runtime): session CRUD resolve messages search rewind"
```

---

### Task 4: Portal Runtime turns（stream + final）

**Files:**
- Modify: `portal/internal/runtime/service.go`
- Create: `portal/internal/runtime/turns_test.go`
- Reuse: `portal/internal/service/chat.go` + `portal/internal/server/chat_sse.go` 逻辑（抽取共享优先）

- [ ] **Step 1: Failing tests**

```go
func TestTurns_FinalReturnsJSON(t *testing.T) { /* reply_mode=final → status ok + content */ }
func TestTurns_StreamSetsSSEHeaders(t *testing.T) { /* text/event-stream */ }
func TestTurns_OwnsSessionACL(t *testing.T) { /* user mismatch → 403 */ }
func TestTurns_CancelContext(t *testing.T) {
	// cancel request ctx mid-stream → runner observes ctx.Done (可用 short sleep stub)
}
```

- [ ] **Step 2: Run — fail**

```bash
cd portal && go test ./internal/runtime/ -run TestTurns -count=1
```

- [ ] **Step 3: Implement**

- `POST /runtime/v1/turns` with `reply_mode=final|stream`
- stream：复用 `chat_sse.go` 事件语义；**绑定传入 ctx**，客户端断开即取消
- **透传** Web body 中的 `confirm_response` / `input_response`（与 `client.ts` `sendMessageStream` 一致），否则 HITL 确认卡断裂
- final：同步 JSON；超时 120s；无交互面时 HITL 阻塞 → `status=failed`


- [ ] **Step 4: Pass + commit**

```bash
cd portal && git commit -am "feat(runtime): turns stream and final modes"
```

---

### Task 5: 关闭对外 Chat 入站

**Files:**
- Modify: `portal/internal/server/http.go`（含 `messages` 与 `messages/stream`）
- Modify: configs：`chat.public_inbound_enabled: false`
- Create: `portal/internal/server/chat_inbound_gate_test.go`

- [ ] **Step 1: Test** — gate=false 时拒绝：
  - `POST /api/v1/sessions/{id}/messages`
  - `POST /api/v1/sessions/{id}/messages/stream`
  - `POST /api/v1/agents/{id}/sessions`（创建）
  Runtime `/runtime/v1/*` 仍可用。wecom/Channel 管理 API 不受影响（可加一条仍 2xx 的冒烟断言）。

- [ ] **Step 2: Implement gate**

- [ ] **Step 3: Commit**

```bash
cd portal && git commit -am "feat(chat): disable public inbound when runtime gateway enabled"
```

---

### Task 6: Gateway module scaffold

**Files:**
- Create: `gateway/go.mod`, `gateway/cmd/gateway/main.go`, `gateway/internal/config/config.go`, `gateway/configs/config.example.yaml`

- [ ] **Step 1: `go mod init github.com/sixath/gateway` + main 打印 version/listen**

- [ ] **Step 2: Config 结构**

```yaml
listen: ":8088"
portal_base_url: "http://127.0.0.1:8000"
runtime_token: "dev-runtime-token"
turn_timeout_sec: 120
channels_file: "./configs/channels.yaml"
# Web 用户鉴权：调用 Portal GET /api/v1/auth/me（不透明 Bearer），无 jwt_secret
```

- [ ] **Step 3: `go build ./cmd/gateway`**

- [ ] **Step 4: Commit (sixath root)**

```bash
git add gateway && git commit -m "feat(gateway): scaffold module and config"
```

---

### Task 7: Gateway ChannelRegistry + RuntimeClient

**Files:**
- Create: `gateway/internal/channel/registry.go`, `registry_test.go`
- Create: `gateway/internal/runtimeclient/client.go`, `client_test.go`

- [ ] **Step 1: Tests** — 加载 channels.yaml；未知 id error；Client 带 Bearer + 可选 `X-Sath-User-Id`。

- [ ] **Step 2: Implement**

```go
type Channel struct {
	ID, Type, DefaultAgent, WebhookSecret string
	IPWhitelist []string
	Enabled bool
	DefaultReplyMode string // async|sync
}
```

Client 方法覆盖 Task 3/4 全部 Runtime 路径（含 stream 返回 `io.ReadCloser`）。

- [ ] **Step 3: Pass + commit**

```bash
git add gateway/internal && git commit -m "feat(gateway): channel registry and runtime client"
```

---

### Task 8: Gateway Webhook adapter（async + sync + 幂等 + 410/IP）

**Files:**
- Create: `gateway/internal/adapter/adapter.go`
- Create: `gateway/internal/adapter/webhook.go`
- Create: `gateway/internal/adapter/webhook_test.go`
- Create: `gateway/internal/reply/dispatcher.go`
- Create: `gateway/internal/session/router.go`（resolve 短缓存）
- Create: `gateway/internal/idempotency/store.go`

- [ ] **Step 1: Tests**

- 错误 secret → 401
- **disabled channel → 410**
- **IP 不在白名单 → 403**（配置非空白名单时）
- 正确请求 → 202；mock Portal → POST reply_url
- 同 idempotency_key 不二次 turns
- `reply_mode=sync` → 200
- Portal failed → reply_url `status=failed`

- [ ] **Step 2: Implement** `Normalize → authz → Resolve → 202 → TurnsFinal → reply_url`

- [ ] **Step 3: Pass + commit**

```bash
git commit -am "feat(gateway): webhook inbound with async reply and idempotency"
```

---

### Task 9: Gateway Web adapter（真实路径表 + SSE cancel）

**Files:**
- Create: `gateway/internal/adapter/web.go`
- Create: `gateway/internal/adapter/web_test.go`
- Modify: `gateway/cmd/gateway/main.go`

**对外路由（必须与 `web/src/api/client.ts` 一致）：**

| Method | Public path | Runtime |
|--------|-------------|---------|
| POST | `/api/v1/agents/{agent_id}/sessions` | create |
| GET | `/api/v1/agents/{agent_id}/sessions` | list by agent |
| GET | `/api/v1/sessions` | list all |
| GET | `/api/v1/sessions/search` | search |
| GET | `/api/v1/sessions/{id}` | get |
| PUT | `/api/v1/sessions/{id}` | update |
| DELETE | `/api/v1/sessions/{id}` | delete |
| GET | `/api/v1/sessions/{id}/messages` | messages |
| POST | `/api/v1/sessions/{id}/messages/stream` | turns stream |
| POST | `/api/v1/sessions/{id}/rewind` | rewind |

- [ ] **Step 1: Tests**

- 无用户 Bearer → 401
- 先 `auth/me` 再 Runtime；转发仅 service token + `X-Sath-User-Id`
- SSE 透传 `data:`；body 可带 `confirm_response`
- **取消客户端请求时 Runtime 侧 ctx 取消**（mock 可观察）

- [ ] **Step 2: Implement** — `auth/me` 解析 user；实现上表全部路由；SSE 代理透传 disconnect cancel。

- [ ] **Step 3: Pass + commit**

```bash
git commit -am "feat(gateway): web adapter for real chat session routes and SSE"
```

---

### Task 10: Compose + Web 代理切流

**Files:**
- Modify: `docker-compose.yml`
- Create: `gateway/Dockerfile`
- Modify: `web/vite.config.ts`
- Modify: web 生产 nginx（若有）
- Modify: `README.md`

- [ ] **Step 1: vite 代理（先匹配会话相关，再 fallback Portal）**

```ts
proxy: {
  '/api/v1/sessions': { target: 'http://localhost:8088', changeOrigin: true },
  '/api/v1/agents': {
    target: 'http://localhost:8088',
    changeOrigin: true,
    bypass(req) {
      // 仅 /agents/:id/sessions* 进 Gateway；Agent CRUD/hub 等回 Portal
      const u = req.url || ''
      if (/^\/api\/v1\/agents\/[^/]+\/sessions/.test(u)) return undefined
      return u
    },
  },
  '/api': { target: 'http://localhost:8000', changeOrigin: true },
}
```

- [ ] **Step 2: compose** — gateway `18088:8088`；env token；web depends_on gateway。

- [ ] **Step 3: 手动烟雾 — 创建会话、流式聊天、webhook 202。

- [ ] **Step 4: Commit root + web**

---

### Task 11: E2E 验收脚本

**Files:**
- Create: `_neo4j_q/verify_inbound_gateway.ps1`

- [ ] **Step 1: 断言**

1. Webhook bad secret → 非 2xx；disabled → 410  
2. Good → 202；reply_url ok  
3. 同 webhook peer 两轮同 session；异 peer 不同  
4. **同一 Web 用户可创建两个 session 且 id 不同**  
5. 直连 Portal `messages/stream` → 拒绝  
6. 经 Gateway `messages/stream` 一轮成功  
7. wecom/Channel 管理 API 仍可用（轻量 GET）

- [ ] **Step 2: Run script → `ok: true`**

- [ ] **Step 3: Commit**

---

## 风险与注意

- **抽取 SSE**：优先从 `chat_sse.go` / `chat.go` 抽共享 `RunTurnStream(ctx, ...)`，ctx 取消必须贯穿。
- **Vite bypass**：`/api/v1/agents` 代理极易误伤 Agent CRUD——以 path 正则限制。
- **鉴权**：Gateway 用 `auth/me`，禁止自造 JWT。
- **嵌套 Git**：portal/web/root 分仓提交；勿提交真实 token。

---

## 执行手顺建议

1→5 Portal Runtime 可独立测通 → 6→9 Gateway → 10 切流 → 11 E2E。