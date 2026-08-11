# Portal 渠道配置真相源 + wecom_bot Runtime Status Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 废弃 Gateway 运行时对 `channels.yaml` 的依赖；Portal/UI 管理 `webhook`/`wecom_bot` 协议配置；Gateway 定时拉取并热更新；`wecom_bot` 上报连接态并在 Channels 列表/编辑页展示。

**Architecture:** Portal 扩展 `channels` 表 + 旁路 `channel_runtime_status`；Admin CRUD 脱敏密钥；Runtime `GET /gateway/channels`（含 disabled + 明文密钥）与 `POST .../status`。Gateway 用可变 `Registry` + `WecomBotManager` 按 diff 启停 runner，~15s 拉配置、~30s/变更心跳。Web 增加 `wecom_bot` 表单与 Runtime Status 列。

**Tech Stack:** Go (Portal Kratos / Gateway stdlib)、MySQL migrations、protobuf `channel.v1`、React Web、现有 `runtimeclient`。

**Spec:** `docs/superpowers/specs/2026-08-11-channel-portal-runtime-status-design.md`

---

## File map

| File | Responsibility |
|------|----------------|
| `portal/migrations/015_channel_gateway_fields.sql` | `wecom_bot` 字段 + `default_reply_mode` + `channel_runtime_status` 表 |
| `portal/internal/data/model/channel.go` | GORM 模型扩展 |
| `portal/internal/data/model/channel_runtime_status.go` | 状态旁路表模型 |
| `portal/internal/biz/channel.go` + `channel_usecase.go` | Create/Update 校验；字段映射 |
| `portal/internal/biz/channel_runtime.go` | `DeriveRuntimeStatus` + upsert 语义 |
| `portal/internal/data/channel_mysql.go` | 读写新列；ListGatewayChannels |
| `portal/internal/data/channel_runtime_mysql.go` | status repo |
| `portal/api/channel/v1/channel.proto` (+ `make api`) | Admin API 字段 + `runtime_status` |
| `portal/internal/service/channel.go` | Create/Update/List/Get 接线；脱敏 |
| `portal/internal/runtime/http.go` + `service.go` | Gateway channels GET + status POST |
| `web/src/api/client.ts` | 类型与 API |
| `web/src/pages/ChannelList.tsx` / `ChannelForm.tsx` | UI |
| `portal/scripts/import_gateway_channels*` 或 `docs/...` | yaml → Portal upsert |
| `gateway/internal/channel/registry.go` | 线程安全 `ReplaceAll` / snapshot |
| `gateway/internal/runtimeclient/client.go` | `ListGatewayChannels` / `ReportChannelStatus` |
| `gateway/internal/channelsync/sync.go` | 拉配置 + diff 应用 |
| `gateway/internal/adapter/wecom_manager.go` | 按 channel 启停 runner + 上报状态 |
| `gateway/cmd/gateway/main.go` | 去掉 `channel.Load(yaml)`；启动 sync |
| `gateway/internal/config/config.go` | `channels_file` 弃用（可选保留忽略） |
| `gateway/README.md` / `docker-compose.yml` / 根 README | 文档与挂载清理 |

---

### Task 1: Portal migration + models

**Files:**
- Create: `portal/migrations/015_channel_gateway_fields.sql`
- Modify: `portal/internal/data/model/channel.go`
- Create: `portal/internal/data/model/channel_runtime_status.go`

- [ ] **Step 1: 写 migration**

```sql
-- portal/migrations/015_channel_gateway_fields.sql
ALTER TABLE channels
  ADD COLUMN bot_id VARCHAR(128) NULL COMMENT 'wecom_bot bot_id' AFTER webhook_url,
  ADD COLUMN bot_secret VARCHAR(256) NULL COMMENT 'wecom_bot secret' AFTER bot_id,
  ADD COLUMN bot_names JSON NULL COMMENT 'wecom_bot mention names' AFTER bot_secret,
  ADD COLUMN ws_url VARCHAR(512) NULL COMMENT 'wecom_bot websocket url' AFTER bot_names,
  ADD COLUMN corp_id VARCHAR(64) NULL AFTER ws_url,
  ADD COLUMN corp_secret VARCHAR(256) NULL AFTER corp_id,
  ADD COLUMN default_reply_mode VARCHAR(16) NULL COMMENT 'async|sync for webhook' AFTER corp_secret;

CREATE TABLE IF NOT EXISTS channel_runtime_status (
  channel_id VARCHAR(64) NOT NULL PRIMARY KEY,
  state VARCHAR(32) NOT NULL,
  last_heartbeat_at DATETIME(3) NOT NULL,
  last_error TEXT NULL,
  reconnect_attempt INT NOT NULL DEFAULT 0,
  reconnect_in_ms INT NOT NULL DEFAULT 0,
  gateway_instance_id VARCHAR(128) NULL,
  updated_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

> DB 列名用 `bot_secret` 避免与通用 `secret` 混淆；API/JSON 对外字段仍用 spec 的 `secret`。

- [ ] **Step 2: 扩展 GORM `Channel` 与新建 `ChannelRuntimeStatus` 模型**

- [ ] **Step 3: Commit**

```bash
git add portal/migrations/015_channel_gateway_fields.sql portal/internal/data/model/
git commit -m "feat(portal): migrate wecom_bot fields and runtime status table"
```

---

### Task 2: DeriveRuntimeStatus + status repo（TDD）

**Files:**
- Create: `portal/internal/biz/channel_runtime.go`
- Create: `portal/internal/biz/channel_runtime_test.go`
- Create: `portal/internal/data/channel_runtime_mysql.go`
- Wire repo in `portal/internal/data/data.go`（或现有 provider）

- [ ] **Step 1: 写失败测试**

```go
func TestDeriveRuntimeStatus_NonWecomBot(t *testing.T) { /* type webhook → nil */ }
func TestDeriveRuntimeStatus_Disabled(t *testing.T) { /* enabled false → disabled */ }
func TestDeriveRuntimeStatus_StaleUnknown(t *testing.T) { /* heartbeat > 90s → unknown */ }
func TestDeriveRuntimeStatus_FreshConnected(t *testing.T) { /* ok → connected */ }
func TestUpsertStatus_ConnectedClearsError(t *testing.T) { /* connected 清空 error/reconnect */ }
func TestUpsertStatus_OmitPreserves(t *testing.T) { /* 省略可选字段保留旧值 */ }
```

常量：`RuntimeStatusStaleAfter = 90 * time.Second`。

- [ ] **Step 2: 实现 `DeriveRuntimeStatus(ch *ChannelMeta, row *RuntimeStatusRow, now time.Time) *RuntimeStatusView`**

派生顺序严格按 spec §3.5。

- [ ] **Step 3: 实现 `ChannelRuntimeRepo.Upsert(ctx, channelID, patch)`**

- `state` 必填；刷新 `last_heartbeat_at=now`
- 可选字段：指针/`*string`/`*int` — nil=保留，非 nil=覆盖
- `state==connected`：强制 `last_error=""`、`reconnect_*=0`

- [ ] **Step 4: 跑测**

```bash
cd portal && go test ./internal/biz/ -run DeriveRuntimeStatus\|UpsertStatus -count=1
```

Expected: PASS

- [ ] **Step 5: 若新增了 data provider，运行 wire 生成（如 `cd portal && make wire` 或项目惯用的 `go generate`），确认 `wire_gen.go` 更新**

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(portal): derive and upsert channel runtime status"
```

---

### Task 3: Channel data layer — 新字段 CRUD + ListGatewayChannels

**Files:**
- Modify: `portal/internal/biz/channel.go`（`ChannelCreate`/`ChannelMeta` 加字段）
- Modify: `portal/internal/data/channel_mysql.go`
- Modify: `portal/internal/biz/channel_usecase.go`（校验）
- Test: `portal/internal/biz/channel_usecase_test.go`（若无则新建）

- [ ] **Step 1: 扩展 Create/Update 映射**

允许字段：`bot_id`, `bot_secret`（updates key 可用 `secret` 再映射）, `bot_names`, `ws_url`, `corp_id`, `corp_secret`, `default_reply_mode`。

校验：

```go
if typ == "wecom_bot" && enabled {
  if botID == "" || botSecret == "" { return BadRequest }
}
if typ == "webhook" && mode != "" && mode != "async" && mode != "sync" { return BadRequest }
```

Update：`secret` / `corp_secret` / `bot_secret` 空字符串 → **不写入** updates（保留原值）。

- [ ] **Step 2: `ListGatewayChannels(ctx)`**

```go
// WHERE type IN ('webhook','wecom_bot')  — 含 enabled=false
// 返回含明文 secrets 的 GatewayChannel DTO
```

- [ ] **Step 3: 单测校验 + List 过滤**

```bash
cd portal && go test ./internal/biz/ ./internal/data/ -count=1
```

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(portal): persist wecom_bot protocol fields and list for gateway"
```

---

### Task 4: Proto + Admin API（含 runtime_status）

**Files:**
- Modify: `portal/api/channel/v1/channel.proto`
- Run: `cd portal && make api`
- Modify: `portal/internal/service/channel.go`
- Test: 扩展现有 channel service/http 测试（若有）

- [ ] **Step 1: Proto 变更**

`CreateChannelRequest` / `ChannelReply` 增加：

- `bot_id`, `secret`（写）, `bot_names`, `ws_url`, `corp_id`, `corp_secret`（写）, `default_reply_mode`
- `ChannelReply`：`secret_set` bool 或 masked 占位；**永不**回传明文 `secret`/`corp_secret`
- `RuntimeStatus` message + `ChannelReply.runtime_status`（optional）
- type 注释含 `wecom_bot`

- [ ] **Step 2: `make api` 重新生成 pb**

- [ ] **Step 3: Service 层**

Create/Update 映射新字段；List/Get 调用 `DeriveRuntimeStatus` 填充；脱敏。

- [ ] **Step 4: 手动或测试验证 Admin JSON 无明文 secret**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(portal): expose wecom_bot and runtime_status on admin channel API"
```

---

### Task 5: Runtime HTTP — list channels + post status

**Files:**
- Modify: `portal/internal/runtime/http.go`
- Modify: `portal/internal/runtime/service.go`
- Modify: `portal/internal/runtime/sessions_test.go` 或新建 `gateway_channels_test.go`
- Wire usecase/repos in runtime Service 构造

- [ ] **Step 1: 写失败 HTTP 测试**

```go
func TestGatewayListChannels_IncludesDisabled(t *testing.T) { /* ... */ }
func TestGatewayListChannels_RequiresToken(t *testing.T) { /* 401 */ }
func TestPostChannelStatus_UnknownChannel404(t *testing.T) { /* ... */ }
func TestPostChannelStatus_ConnectedClearsError(t *testing.T) { /* ... */ }
```

- [ ] **Step 2: 注册路由**

```go
r.GET("/runtime/v1/gateway/channels", svc.wrap(svc.handleListGatewayChannels))
r.POST("/runtime/v1/gateway/channels/{channel_id}/status", svc.wrap(svc.handlePostChannelStatus))
```

响应形状对齐 spec §4.1 / §4.2（`id` = `channel_id`）。

- [ ] **Step 3: Post status 前 `GetByChannelID`；不存在 → 404**

- [ ] **Step 4: 跑测**

```bash
cd portal && go test ./internal/runtime/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(portal): runtime gateway channel config and status endpoints"
```

---

### Task 6: Web — types、列表 Runtime Status、表单 wecom_bot

**Files:**
- Modify: `web/src/api/client.ts`
- Modify: `web/src/pages/ChannelList.tsx`
- Modify: `web/src/pages/ChannelForm.tsx`

- [ ] **Step 1: 扩展 `Channel` / `CreateChannelRequest`**

含 `runtime_status?`、`bot_id`、`bot_names`、`ws_url`、`default_reply_mode`、`secret_set` 等；type 联合加 `'wecom_bot'`。

- [ ] **Step 2: ChannelList**

- 列 **Runtime Status**：`wecom_bot` 显示 state（色点），其它 `—`
- Admin 列仍用 `enabled`
- type filter 加 `wecom_bot`
- 保留/强化 Refresh（`loadChannels`）

- [ ] **Step 3: ChannelForm**

`wecom_bot` 字段块；`webhook` 的 `default_reply_mode`；secret 空=不提交。

Edit 页底部 Runtime 面板（仅 `wecom_bot`）：state、heartbeat 相对时间、error、reconnect。

- [ ] **Step 4: 本地打开 `/channels` 冒烟（需 Portal 已迁移）**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(web): wecom_bot channel form and runtime status column"
```

---

### Task 7: yaml → Portal upsert import

**Files:**
- Create: `portal/cmd/import-gateway-channels/main.go`（推荐可执行）**或** `scripts/import_gateway_channels.go`
- Create: 简短用法说明（脚本 `-h` / `docs/superpowers/specs/...` 链到 README）

- [ ] **Step 1: 实现**

读取 yaml（与 Gateway `channel.Channel` 同结构），对每个条目：

1. `GetByChannelID`
2. 不存在 → Create（type/webhook|wecom_bot 字段；若 yaml 有 `default_agent` 则写入）
3. 存在 → Update **协议字段**（至少：`type`、`enabled`、`webhook_secret`、`ip_whitelist`、`default_reply_mode`、`bot_id`/`secret`/`bot_names`/`ws_url`/`corp_*`，对齐 spec §5.3）；yaml `default_agent` 非空才覆盖；`allowed_agents` 同理（yaml 无则保留）

- [ ] **Step 2: 对本地 `gateway/configs/channels.yaml` dry-run / 实跑一次（开发库）**

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(portal): upsert import gateway channels.yaml into portal"
```

---

### Task 8: Gateway — mutable Registry + runtimeclient

**Files:**
- Modify: `gateway/internal/channel/registry.go`
- Create: `gateway/internal/channel/registry_test.go`（扩展）
- Modify: `gateway/internal/runtimeclient/client.go`
- Modify: `gateway/internal/runtimeclient/client_test.go`

- [ ] **Step 1: Registry 线程安全**

```go
type Registry struct {
  mu   sync.RWMutex
  byID map[string]Channel
}

func (r *Registry) ReplaceAll(chs []Channel) { /* swap map */ }
func (r *Registry) Snapshot() []Channel { /* copy */ }
// Get / All 加 RLock
```

保留 `Load(path)` 供 import 工具/测试；Gateway main **不再**调用。

- [ ] **Step 2: runtimeclient**

```go
func (c *Client) ListGatewayChannels(ctx context.Context) ([]channel.Channel, error)
func (c *Client) ReportChannelStatus(ctx context.Context, channelID string, body StatusBody) error
```

- [ ] **Step 3: 跑测**

```bash
cd gateway && go test ./internal/channel/ ./internal/runtimeclient/ -count=1
```

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(gateway): mutable registry and portal channel/status client"
```

---

### Task 9: Gateway — sync loop + WecomBotManager + status 上报

**Files:**
- Create: `gateway/internal/channelsync/sync.go` + `_test.go`
- Create: `gateway/internal/adapter/wecom_manager.go`（可从 `wecom_bot.go` 抽出启停）
- Modify: `gateway/internal/adapter/wecom_bot.go`（单次 run + 回调状态）
- Modify: `gateway/cmd/gateway/main.go`
- Modify: `gateway/internal/config/config.go`（`channels_file` 标记废弃；可忽略）
- Test: diff 启停与失败保留旧配置

- [ ] **Step 1: `channelsync.Runner`**

```go
// 周期 15s：ListGatewayChannels → diff
// 失败：log，不 ReplaceAll
// 成功：ReplaceAll(registry) + notify manager
```

Diff 行为严格按 spec §5.2。

- [ ] **Step 2: `WecomBotManager`**

- 每 channel 独立 `context.CancelFunc`
- start/stop/restart
- 在 `runWecomBotOnce` 成功连上 → `ReportChannelStatus(connected)`
- `runWecomBotOnce` **返回错误即将进入 backoff 前** → 先报 `disconnected`（带 `last_error`），随后进入等待时改报 / 保持为 `reconnecting`（`reconnect_attempt`、`reconnect_in_ms`）
  - 验收 #4：列表/详情应能看到 Disconnected **或** Reconnecting，以及 error + 重连字段；实现上两态都允许短暂出现
- disabled/stop → `disabled`
- 另起 ticker ~30s 对当前 connected 心跳

- [ ] **Step 3: 改 `main.go`**

```go
reg := channel.NewRegistry() // empty
// start sync + manager
// webhook handler 仍用同一 reg（Get 动态）
// 删除 channel.Load(cfg.ChannelsFile) 失败即 Fatal
```

启动：sync 立刻拉一次；失败则重试，进程不退出。

- [ ] **Step 4: 单元测 sync diff（假 client）**

```bash
cd gateway && go test ./internal/channelsync/ ./internal/adapter/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(gateway): sync channels from portal and report wecom_bot status"
```

---

### Task 10: Docs + compose 去 yaml 运行时依赖

**Files:**
- Modify: `gateway/README.md`
- Modify: `docker-compose.yml`（去掉 channels.yaml volume；必要时加注释）
- Modify: 根 `README.md` 相关步骤
- Modify: `gateway/configs/channels.yaml` 顶部注释改为「归档/import 用，非运行时」
- Modify: `gateway/configs/config.example.yaml`（`channels_file` 废弃说明）
- Modify: `gateway/configs/config.docker.yaml`（若存在 `channels_file`，同步废弃/删除）

- [ ] **Step 1: 文档写清**

- 配置源 = Portal
- import 命令
- Runtime Status 五态与 90s Unknown
- 同一 `bot_id` 勿多实例

- [ ] **Step 2: Commit**

```bash
git commit -m "docs: portal owns gateway channels; retire channels.yaml runtime"
```

---

### Task 11: 端到端验收（手工清单）

对照 spec §8：

- [ ] UI 新建 `wecom_bot` → ≤15s Connected
- [ ] 停 Gateway → ≤90s Unknown
- [ ] disabled → Disabled + runner 停
- [ ] 错 secret / 断线 → error + reconnecting
- [ ] webhook Runtime 列 —
- [ ] 改 secret → 重连 Connected
- [ ] 无 yaml 挂载重启 Gateway 仍工作
- [ ] disabled webhook → 410；删除渠道 → 404

- [ ] **全部通过后**：把 spec 状态改为「已实现」或在 plan 顶部勾选完成（可选 commit）

---

## 风险与注意

- **Proto 生成**：改 `channel.proto` 后必须 `make api`，勿手改 `*.pb.go`。
- **列名**：DB `bot_secret` ↔ API `secret` 映射不要弄反。
- **热更新**：变更 bot 凭证必须先 cancel 旧 runner，再 start，避免企微侧双连接踢线循环。
- **测试环境**：import 前确认 Portal DB 已跑 migration 015。

---

## 执行方式

实现时推荐 **subagent-driven-development**（每 Task 新 subagent + 双阶段审查），或本会话 **executing-plans** 按 checkpoint 推进。
