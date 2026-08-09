# Inbound Gateway Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地独立 Gateway + Portal `/runtime/v1` 契约，使 Web 与通用 Webhook 统一入站对话；关闭对外直连 Chat 入站。

**Architecture:** Gateway 终止用户/Webhook 鉴权，调用 Portal Runtime（service token）；Webhook 用 `channel+peer` 映射续聊；Web 保持多 session UX，经 Gateway 代理会话 CRUD + SSE；Portal 既有 Channel/wecom 出站不动。权威规格：[`2026-08-09-inbound-gateway-design.md`](../specs/2026-08-09-inbound-gateway-design.md)。

**Tech Stack:** Go 1.25（`gateway/` 新 module + `portal/` Kratos）、React/Vite（`web/` 代理切流）、Docker Compose、MySQL（peer 映射表）。

**Repos:** `gateway/` 落在 sixath 编排仓；`portal/`、`web/` 为嵌套仓——改动在对应目录提交。**Do not commit unless asked**（文档仓提交除外若用户已要求）。

**Locked decisions (from §9):**

| 项 | 锁定 |
|----|------|
| Web 代理切面 | Gateway 对外兼容现有 `/api/v1/sessions*` 对话路径；其余 `/api`（Agent/Tool/Channel 管理等）仍直连 Portal |
| `peer_id`（Web） | 登录用户 ID，仅 ACL；不折叠多会话 |
| `peer_id`（Webhook） | 请求体字段；`channel+peer` → 唯一 session |
| Service token | 配置项 `runtime.service_token` / env `SATH_RUNTIME_TOKEN`；compose 与本地默认 `dev-runtime-token` |
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
| `portal/internal/runtime/service.go` | Resolve / CreateSession / List / Turns stream\|final |
| `portal/internal/runtime/http.go` | 注册 `/runtime/v1/*` |
| `portal/internal/server/http.go` | 挂 Runtime；Chat 公共入站按开关拒绝 |
| `portal/configs/*.yaml` | `runtime.service_token`、`chat.public_inbound_enabled: false` |
| `gateway/go.mod` | module `github.com/sixath/gateway` |
| `gateway/cmd/gateway/main.go` | 进程入口 |
| `gateway/internal/config/config.go` | Portal URL、token、listen、channels 文件 |
| `gateway/internal/channel/registry.go` | 渠道配置加载 |
| `gateway/internal/runtimeclient/client.go` | Portal Runtime HTTP 客户端 |
| `gateway/internal/session/router.go` | resolve 缓存（webhook） |
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
cd portal && git add internal/data/model/channel_peer_session.go internal/biz/channel_peer.go internal/data/channel_peer_mysql.go internal/data/data.go && git commit -m "feat(runtime): persist channel+peer session mapping"
```

---

### Task 2: Portal Runtime auth 中间件

**Files:**
- Create: `portal/internal/runtime/auth.go`
- Create: `portal/internal/runtime/auth_test.go`
- Modify: `portal/configs/config.yaml`、`config.docker.yaml`（及 conf proto/struct 若项目用结构化配置）

- [ ] **Step 1: Failing tests**

```go
func TestRuntimeAuth_RejectsMissingToken(t *testing.T) { /* 401 */ }
func TestRuntimeAuth_AcceptsBearerAndSetsUser(t *testing.T) {
	// Authorization: Bearer <token>; X-Sath-User-Id: u1 → ctx has user
}
```

- [ ] **Step 2: Run — expect fail**

```bash
cd portal && go test ./internal/runtime/ -run TestRuntimeAuth -count=1
```

- [ ] **Step 3: Implement**

校验 `Authorization: Bearer` == 配置 `runtime.service_token`；可选读 `X-Sath-User-Id` 写入 context（Web 路径必填；Webhook resolve 可空或用 peer）。

- [ ] **Step 4: Pass + commit**

```bash
cd portal && git commit -am "feat(runtime): service token auth for /runtime/v1"
```

---

### Task 3: Portal Runtime HTTP — sessions + resolve

**Files:**
- Create: `portal/internal/runtime/service.go`
- Create: `portal/internal/runtime/http.go`
- Create: `portal/internal/runtime/sessions_test.go`
- Modify: `portal/internal/server/http.go`（注册路由，走 Runtime auth）

- [ ] **Step 1: Contract tests（httptest）**

覆盖：
- `POST /runtime/v1/sessions/resolve` 同 peer 同 session
- `POST /runtime/v1/sessions` 创建；`GET` 列表按 user
- 无 token → 401

- [ ] **Step 2: Run — fail**

```bash
cd portal && go test ./internal/runtime/ -count=1
```

- [ ] **Step 3: Implement handlers**

委托现有 `ChatUsecase` / `ChannelPeer` Resolve。响应 JSON 字段与现有 `SessionReply` 对齐（id、agent_id、title、timestamps），降低 Gateway/Web 摩擦。

- [ ] **Step 4: Pass + commit**

```bash
cd portal && git commit -am "feat(runtime): session resolve and CRUD endpoints"
```

---

### Task 4: Portal Runtime turns（stream + final）

**Files:**
- Modify: `portal/internal/runtime/service.go`
- Create: `portal/internal/runtime/turns_test.go`
- Reuse: `portal/internal/service/chat.go` 内 SendMessage/SSE 逻辑（抽取共享函数优先于复制）

- [ ] **Step 1: Failing tests**

```go
func TestTurns_FinalReturnsJSON(t *testing.T) {
	// mock agent runner or short-circuit: reply_mode=final → status ok + content
}
func TestTurns_StreamSetsSSEHeaders(t *testing.T) {
	// Accept handling / Content-Type text/event-stream
}
func TestTurns_OwnsSessionACL(t *testing.T) {
	// X-Sath-User-Id mismatch → 403
}
```

- [ ] **Step 2: Run — fail**

```bash
cd portal && go test ./internal/runtime/ -run TestTurns -count=1
```

- [ ] **Step 3: Implement**

- `reply_mode=final`：复用非流式 SendMessage 路径，聚合 assistant 文本；超时 context 120s。
- `reply_mode=stream`：复用现有 SSE 事件写入 ResponseWriter（与 `SendMessage` Accept 流式同语义）。
- Webhook 调用可不传 user；用 mapping 的 session；HITL 危险确认在 final 路径 fail-closed（若现有 confirm 阻塞，返回 `status=failed` + 明确 error 文案）。

- [ ] **Step 4: Pass + commit**

```bash
cd portal && git commit -am "feat(runtime): turns stream and final modes"
```

---

### Task 5: 关闭对外 Chat 入站

**Files:**
- Modify: `portal/internal/server/http.go` 或 Chat 服务包装
- Modify: configs：`chat.public_inbound_enabled: false`
- Create: `portal/internal/server/chat_inbound_gate_test.go`

- [ ] **Step 1: Test** — `public_inbound_enabled=false` 时 `POST /api/v1/sessions/{id}/messages` → 403/404；`/runtime/v1/turns` 仍可用。

- [ ] **Step 2: Implement gate** — 仅拦截**用户对话写路径**（SendMessage 及如需 CreateSession 对外）。管理 API、Channel CRUD、wecom 出站保持。若测试依赖旧路径，用配置在 test 中开启。

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
# web auth: reuse portal JWT secret or gateway-local validator config
auth:
  jwt_secret: "..." # 与 portal 对齐，或调用 portal introspect（一期共享 secret）
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
- Create: `gateway/internal/runtimeclient/client.go`, `client_test.go`（httptest mock Portal）

- [ ] **Step 1: Tests** — 加载 channels.yaml；未知 id error；disabled → 标记；Client Resolve/TurnsFinal/TurnsStream 发正确 header。

- [ ] **Step 2: Implement**

```go
type Channel struct {
	ID, Type, DefaultAgent, WebhookSecret string
	IPWhitelist []string
	Enabled bool
	DefaultReplyMode string // async|sync
}
```

- [ ] **Step 3: Pass + commit**

```bash
git add gateway/internal && git commit -m "feat(gateway): channel registry and runtime client"
```

---

### Task 8: Gateway Webhook adapter（async + sync + 幂等）

**Files:**
- Create: `gateway/internal/adapter/adapter.go`
- Create: `gateway/internal/adapter/webhook.go`
- Create: `gateway/internal/adapter/webhook_test.go`
- Create: `gateway/internal/reply/dispatcher.go`
- Create: `gateway/internal/idempotency/store.go`（进程内 map + TTL 即可一期）

- [ ] **Step 1: Tests**

- 错误 secret → 401
- 正确请求 → 202 + correlation_id；mock Portal final ok → POST reply_url 含 content
- 同 idempotency_key → 不二次 turns
- `reply_mode=sync` → 200 + body（短超时测试）
- Portal failed → reply_url `status=failed`

- [ ] **Step 2: Implement pipeline**

`Normalize → Resolve → 202 → goroutine TurnsFinal → ReplyDispatcher`

- [ ] **Step 3: Pass + commit**

```bash
git commit -am "feat(gateway): webhook inbound with async reply and idempotency"
```

---

### Task 9: Gateway Web adapter（鉴权 + sessions 代理 + SSE）

**Files:**
- Create: `gateway/internal/adapter/web.go`
- Create: `gateway/internal/adapter/web_test.go`
- Modify: `gateway/cmd/gateway/main.go`（挂路由）

**对外路由（兼容 Web client）：**

| Method | Path | Runtime |
|--------|------|---------|
| POST | `/api/v1/sessions` | create |
| GET | `/api/v1/sessions` / list variants | list |
| GET/PATCH/DELETE | `/api/v1/sessions/{id}` | get/update/delete |
| GET | `/api/v1/sessions/{id}/messages` | messages |
| POST | `/api/v1/sessions/{session_id}/messages` | turns stream（SSE） |

- [ ] **Step 1: Tests** — 无用户 token → 401；有 token 时转发 `Authorization` 换 service token + `X-Sath-User-Id`；SSE 透传至少一行 `data:`。

- [ ] **Step 2: Implement** — 复用 portal 同款 JWT 校验（共享 secret）；**不要**把浏览器 cookie/JWT 原样当 Runtime 信任根。

- [ ] **Step 3: Pass + commit**

```bash
git commit -am "feat(gateway): web adapter proxying chat sessions and SSE"
```

---

### Task 10: Compose + Web 代理切流

**Files:**
- Modify: `docker-compose.yml`
- Create: `gateway/Dockerfile`
- Modify: `web/vite.config.ts`
- Modify: `web` 生产 nginx 配置（`web/nginx.conf` 或现有文件；若无则 Dockerfile 内）
- Modify: `README.md` 端口表（gateway）

- [ ] **Step 1: vite 双代理**

```ts
proxy: {
  '/api/v1/sessions': { target: 'http://localhost:8088', changeOrigin: true },
  '/api': { target: 'http://localhost:8000', changeOrigin: true },
}
```

注意：更具体的 `/api/v1/sessions` 必须写在通用 `/api` **之前**（Vite 按序匹配）。

- [ ] **Step 2: compose** — `gateway` 服务暴露 `18088:8088`；env 注入 token；depends_on portal；web depends_on gateway。

- [ ] **Step 3: 手动烟雾** — portal + gateway 起来后，curl webhook 202；浏览器登录聊天仍流式。

- [ ] **Step 4: Commit**

```bash
git add docker-compose.yml gateway README.md
# web 仓单独 commit vite/nginx
cd web && git commit -am "chore: proxy chat sessions via inbound gateway"
```

---

### Task 11: E2E 验收脚本

**Files:**
- Create: `_neo4j_q/verify_inbound_gateway.ps1`
- Create: `_neo4j_q/verify_inbound_gateway_out.json`（运行产物，可不入库）

- [ ] **Step 1: 脚本断言**

1. Webhook bad secret → 非 2xx  
2. Good secret → 202  
3. 同 peer 两轮 → 同 `session_id`（查 Portal DB 或 Runtime get）  
4. 异 peer → 不同 session  
5. `reply_url` 收到 ok（用本地 httptest 或临时 listener）  
6. `POST` 旧 Portal messages 直连 → 拒绝  
7. 经 Gateway SSE/final 一轮成功  

- [ ] **Step 2: Run**

```powershell
powershell -File _neo4j_q/verify_inbound_gateway.ps1
```

Expected: 写出 `ok: true` JSON。

- [ ] **Step 3: Commit script + docs note**

```bash
git add _neo4j_q/verify_inbound_gateway.ps1 docs/superpowers/plans/2026-08-09-inbound-gateway.md
git commit -m "test: add inbound gateway e2e smoke script"
```

---

## 风险与注意

- **抽取 SSE**：优先从 `portal/internal/service/chat.go` 抽共享 `RunTurnStream`，避免 Runtime 与旧 Chat 分叉两套事件。
- **JWT 共享**：Gateway 与 Portal 必须同一校验密钥；文档写进 `gateway/configs` 注释。
- **SearchSessions 等**：若 Web 用到会话搜索且路径不在 `/api/v1/sessions` 前缀下，补进 Gateway 代理表（实现时对照 `web/src/api/client.ts` 一次性扫全）。
- **嵌套 Git**：portal/web/gateway(root) 分仓提交，勿把 `.env` / 真实 token 入库。

---

## 执行手顺建议

1→5 Portal Runtime 可独立测通 → 6→9 Gateway → 10 切流 → 11 E2E。每 Task 结束后保持主路径可编译。
