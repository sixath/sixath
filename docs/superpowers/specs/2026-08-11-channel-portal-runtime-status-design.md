# Portal 渠道配置真相源 + wecom_bot Runtime Status

**日期**: 2026-08-11  
**状态**: 已在分支 `feat/channel-portal-runtime-status` 实现；待手工 E2E（spec §8）与合并  
**目标**: 废弃 Gateway `channels.yaml` 作为运行时真相源；Portal/UI 可创建与管理入站渠道（含 `wecom_bot`）；Gateway 定时拉取配置并热更新；`wecom_bot` 上报运行连接态，Web Channels 列表展示 Runtime Status，编辑页展示运维细节。

**关联**:
- [企微智能机器人设计](./2026-08-09-wecom-bot-gateway-design.md)
- Gateway README：`gateway/README.md`
- Portal Channels Admin API / Web `/channels`

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 配置真相源 | **Portal**；运行时 **不再读取** `channels.yaml` |
| 落地范围 | **一次完成**：UI 管配置 + Gateway 拉配置 + Runtime Status |
| 配置同步 | Gateway **~15s** 全量拉取（**含 disabled**）+ 按 `channel_id` diff 热更新 |
| yaml 迁移 | 按 `channel_id` **upsert**（合并协议字段；保留未在 yaml 给出的 agent 绑定） |
| 状态上报 | 连接态 **立即**上报 + **~30s** 心跳；Portal 存态 |
| Unknown | **90s** 无有效心跳 → `unknown` |
| Runtime Status 范围 | **仅 `wecom_bot`**；其它类型列表显示 `—` |
| 状态枚举 | `connected` / `disconnected` / `reconnecting` / `disabled` / `unknown` |
| 详情字段 | 最近心跳、最近错误、重连次数与下次等待 |
| UI | 列表加 Runtime Status 列；Edit 页 Runtime 面板；手动 Refresh（无 SSE） |
| 类型区分 | `wecom`（群机器人 Webhook）保持不变；新增 `wecom_bot`（智能机器人长连接） |
| 密钥 | Admin API 脱敏；Runtime API（Gateway Token）可读明文配置 |

不在本轮：其它渠道类型的连接态语义、配置推送通知、多 Gateway 抢 `bot_id` 分布式锁、SSE 实时推送状态。

---

## 1. 问题

已有能力：

- Web `/channels`：列表/创建/编辑 Portal 渠道（`web` / `api` / `webhook` / `wxpusher` / `wecom`）
- Gateway：从 `channels.yaml` 加载 `webhook` / `wecom_bot`，企微长连接状态仅打日志
- Agent 路由等业务已以 Portal `channel_id` 为准，但协议密钥仍在 yaml

缺口：

1. 无法在 UI 新建/修改 `wecom_bot`（及完整 Gateway webhook 协议配置），改配置常需改文件并重启
2. 运维看不到 Bot 是否在线；改完配置无法确认 Gateway 是否真正连上
3. yaml 与 Portal 双源，易漂移

---

## 2. 架构

```text
┌─────────────┐   CRUD (Admin)              ┌──────────────────┐
│  Web Admin  │ ───────────────────────────▶│      Portal      │
│ /channels   │ ◀── list + runtime_status ──│  channels +      │
└─────────────┘                              │  runtime status  │
                                             └────────┬─────────┘
                            GET gateway channels ~15s  │
                            POST .../status (heartbeat)│
                                                       ▼
                                             ┌──────────────────┐
                                             │     Gateway      │
                                             │ in-memory        │
                                             │ Registry +       │
                                             │ wecom_bot runners│
                                             └──────────────────┘
```

- **Web → Portal**：渠道 CRUD（含 `wecom_bot` 凭证）
- **Gateway → Portal**：定时拉配置；状态变化 + 心跳写运行态
- **Web → Portal**：读配置与派生后的 `runtime_status`

---

## 3. 数据模型

### 3.1 渠道类型

| type | 含义 | Runtime Status |
|------|------|----------------|
| `wecom` | 现有群机器人 Webhook | 无（`—`） |
| `wecom_bot` | 企微智能机器人长连接（原 yaml） | 有 |
| `webhook` | Gateway HTTP 入站 | 无（`—`） |
| 其它已有类型 | 不变 | 无（`—`） |

### 3.2 `wecom_bot` 配置字段（Portal `channels` 扩展）

| 字段 | 说明 |
|------|------|
| `bot_id` | 必填（enabled 时） |
| `secret` | 必填（enabled 时）；Admin 读脱敏，写明文；空更新=不改 |
| `bot_names` | 字符串数组；@ 机器人去名前缀等 |
| `ws_url` | 可选；默认企微官方 WSS |
| `corp_id` / `corp_secret` | 可选；通讯录显示名 |
| 已有 | `channel_id`, `enabled`, `default_agent`, `allowed_agents` |

### 3.3 `webhook` 补齐

Portal 已有 path/secret/ip 等；若缺 Gateway 所需 `default_reply_mode`（`async`\|`sync`），本轮一并补上。

### 3.4 Runtime Status 存储

按 `channel_id` 存储（旁路表 `channel_runtime_status` 或 channels 扩展列，实现时二选一；推荐旁路表以免污染配置行）：

| 字段 | 说明 |
|------|------|
| `state` | Gateway 上报：`connected` \| `disconnected` \| `reconnecting` \| `disabled` |
| `last_heartbeat_at` | 每次上报刷新 |
| `last_error` | 可选摘要 |
| `reconnect_attempt` | 可选 |
| `reconnect_in_ms` | 可选 |
| `gateway_instance_id` | 可选，诊断用 |

Gateway **不上报** `unknown`；由 Portal 读出时派生。

### 3.5 派生规则（读路径）

1. `type != wecom_bot` → 无 `runtime_status`（UI：`—`）
2. `enabled == false` → 展示 `disabled`
3. 无状态行，或 `now - last_heartbeat_at > 90s` → `unknown`
4. 否则使用上报的 `state`

---

## 4. API

### 4.1 Runtime：拉配置（Gateway）

`GET /runtime/v1/gateway/channels`

- 鉴权：现有 Runtime service token
- 返回 Gateway 需要的渠道类型：**至少包含 `webhook` + `wecom_bot`**
- **必须包含 `enabled: false` 的渠道**（与 yaml 行为一致：disabled webhook 仍留在 Registry，入站返回 **410**；diff 才能区分「禁用」与「已从 Portal 删除」）
- **含密钥明文**（`webhook_secret`、`secret`、`corp_secret` 等）
- 可含 `updated_at` 便于日志；本轮允许全量拉取（~15s）

示例（字段名与 Gateway 内存 `Channel` 对齐；`id` = Portal `channel_id`）：

```json
{
  "channels": [
    {
      "id": "demo-webhook",
      "type": "webhook",
      "enabled": true,
      "webhook_secret": "...",
      "ip_whitelist": [],
      "default_reply_mode": "async",
      "updated_at": "2026-08-11T03:00:00Z"
    },
    {
      "id": "sixath4",
      "type": "wecom_bot",
      "enabled": true,
      "bot_id": "...",
      "secret": "...",
      "bot_names": ["sixath"],
      "ws_url": "wss://openws.work.weixin.qq.com",
      "corp_id": "",
      "corp_secret": "",
      "updated_at": "2026-08-11T03:00:00Z"
    }
  ]
}
```

> Agent 路由仍以 Portal Admin/Runtime 既有接口为准；本接口侧重 **协议连接配置**，不要求重复返回 `default_agent`（Gateway Resolve 已走 Portal）。

### 4.2 Runtime：写状态（Gateway）

`POST /runtime/v1/gateway/channels/{channel_id}/status`

```json
{
  "state": "connected|disconnected|reconnecting|disabled",
  "last_error": "...",
  "reconnect_attempt": 3,
  "reconnect_in_ms": 8000,
  "gateway_instance_id": "gw-1"
}
```

- 状态变化：**立即**调用
- 常态：约 **30s** 心跳（即使 state 未变）
- 未知 `channel_id`：**404**（不创建幽灵状态行）
- 字段语义：
  - 每次成功写入刷新 `last_heartbeat_at`
  - `state` **必填**
  - 可选字段：请求里 **出现则覆盖**；**省略则保留**库中旧值
  - 当 `state=connected` 时，实现应 **清空** `last_error`，并将 `reconnect_attempt` / `reconnect_in_ms` 置 **0**（即使请求省略这些字段）

### 4.3 Admin：列表/详情附带状态

现有 `GET /channels`、`GET /channels/{id}` 增加：

```json
"runtime_status": {
  "state": "connected",
  "last_heartbeat_at": "2026-08-11T03:00:00Z",
  "last_error": "",
  "reconnect_attempt": 0,
  "reconnect_in_ms": 0
}
```

非 `wecom_bot`：`runtime_status` 为 `null`。Admin 响应中 `secret` / `corp_secret` **脱敏或不返回明文**。

### 4.4 Admin：创建/更新

- `type` 允许 `wecom_bot`
- 校验：enabled 的 `wecom_bot` 必须有 `bot_id` + `secret`（更新时 secret 空表示保留原值）
- `webhook` 可写 `default_reply_mode`

---

## 5. Gateway 行为

### 5.1 启动与同步循环

1. 启动时拉取配置；失败则 **重试**，保留空或上一份 Registry，**不因单次失败退出进程**（启动期持续重试直至成功亦可接受）
2. 之后约 **15s** 全量再拉
3. 拉失败：**保留上一份** Registry，打日志

### 5.2 Diff 热更新

以「上一份快照」与「本次拉取」按 `id`（`channel_id`）比较：

| 变化 | 行为 |
|------|------|
| 新增且 `enabled` 的 `wecom_bot` | 启动 runner；**dial 前**立即上报 `reconnecting`（避免 UI 短暂显示 stale `disabled`/`unknown`） |
| `wecom_bot`：`enabled` true→false | **停止** runner；上报 `disabled`；条目仍留在 Registry |
| `wecom_bot`：`enabled` false→true | 启动 runner；**dial 前**立即上报 `reconnecting` |
| `bot_id` / `secret` / `ws_url` / `corp_*` / `bot_names` 等影响连接的字段变更（仍为 enabled） | **先停再启**；新 runner **dial 前**上报 `reconnecting`（避免短暂显示 stale `connected`） |
| 本次拉取中 **消失**（Portal 已删） | 停止 runner（若有）；从 Registry **移除**；不再上报 status |
| `webhook`：仍在列表（含 disabled） | 更新 Registry 条目（disabled 保留以支持 **410**） |
| `webhook`：从列表消失 | 从 Registry 移除（未知 id → 既有 404 行为） |

### 5.3 废弃 yaml

- 运行时 **不读取** `channels.yaml`
- 文档与 compose 去掉对该文件的运行时依赖
- 提供一次性 **import**（脚本或文档步骤）：按 yaml 的 `id` → Portal `channel_id` **upsert**
  - **已存在**：合并/覆盖协议字段（`type`、secrets、`bot_*`、`ip_whitelist`、`default_reply_mode`、`enabled` 等）；**不盲目清空**已有 `default_agent` / `allowed_agents`（yaml 有值则写入，yaml 缺省则保留 Portal 原值）
  - **不存在**：创建新渠道行
- 仓库可保留示例 yaml 仅作归档/对照，明确标注非运行时配置源

### 5.4 多实例

同一 `bot_id` **不得**在多个 Gateway 实例同时 `enabled`（文档约束；可选 Portal 侧警告）。本轮不做分布式锁。

---

## 6. Web UI

### 6.1 列表

- 保留 Admin Enabled/Disabled 列
- 新增 **Runtime Status** 列（`wecom_bot` 五态色点；其它类型 `—`）
- type 筛选增加 `wecom_bot`
- **Refresh** 重拉（无 SSE）

### 6.2 New / Edit

- type 增加 `wecom_bot` 及对应表单字段
- `webhook` 补 `default_reply_mode`（若 UI 尚无）
- secret：空提交=不修改

### 6.3 Edit Runtime 面板（仅 `wecom_bot`）

- State
- Last heartbeat（相对时间）
- Last error
- Reconnect attempt + in Xs

---

## 7. 迁移步骤（实现/上线顺序建议）

1. Portal：schema + Admin/Runtime API + 派生逻辑
2. Web：`wecom_bot` 表单 + Runtime Status 展示
3. Import 现有 yaml → Portal
4. Gateway：改拉 Portal + 热更新 + 状态上报；去掉 yaml Load
5. 更新 README / compose；验证验收用例

---

## 8. 验收

| # | 场景 | 期望 |
|---|------|------|
| 1 | UI 新建 enabled `wecom_bot` | ≤15s Gateway 建立连接；列表 **Connected** |
| 2 | 停止 Gateway 进程 | ≤90s 状态 → **Unknown** |
| 3 | 渠道改为 disabled | 展示 **Disabled**；runner 停止 |
| 4 | 断线 / 错 secret | 可见 **Disconnected/Reconnecting**、`last_error`、重连信息 |
| 5 | 非 `wecom_bot` | Runtime Status 为 **—** |
| 6 | 改 bot secret 后保存 | Gateway 重连；恢复后 **Connected** |
| 7 | 删除 yaml 挂载后重启 Gateway | 仍能从 Portal 加载渠道并工作 |
| 8 | disabled `webhook` 仍在 Portal | Gateway Registry 保留；入站 **410** |
| 9 | 从 Portal **删除** 渠道 | Gateway Registry 移除；webhook 未知 id → **404** |

---

## 9. 测试要点

- Portal：派生规则单测（disabled / stale → unknown / 非 wecom_bot）
- Portal：Runtime status upsert；Admin 脱敏
- Gateway：config diff 启停；拉配置失败保留旧 Registry
- Gateway：status 上报在 connected / reconnecting / disabled 路径
- Web：列表列与表单字段（可组件/集成测按仓库惯例）

---

## 10. 文档

- 更新 `gateway/README.md`：配置源改为 Portal；yaml 废弃说明；Runtime Status 含义
- 更新根 README / compose 注释中与 `channels.yaml` 相关的运行时说明
- Import 步骤写入 docs 或脚本 README
