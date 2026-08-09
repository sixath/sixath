# 入站 Gateway — Web 与多渠道统一接入

**日期**: 2026-08-09  
**状态**: 设计已确认；待实现规划  
**目标**: 引入独立 Gateway 进程，作为 Web 与外部渠道（一期通用 Webhook；后续微信/企微等）的统一入站对话入口；Portal 专注 Agent Runtime 执行。

**关联**:
- [Portal 架构设计 · Channel](../../../portal/docs/architecture_design.md)（§6.6 渠道路由草图；本稿取代「Webhook 直接挂 Portal」为权威入站路径）
- [Harness Engineering 差距](./2026-07-11-harness-engineering-gap-design.md)（S4 Gateway / 多 IM 原标近季不做；本稿重启为可分期交付的入站脊柱）
- [企微群机器人](../../../portal/docs/superpowers/specs/2026-06-04-wecom-bot-design.md)（现有**出站**能力；一期不迁入 Gateway）

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 主问题 | **统一入站对话**（外部/Web 消息进同一套 Agent 会话） |
| 一期渠道 | **通用 Webhook + Web 走同一入站路径**；具体 IM 适配器后挂 |
| 部署形态 | **独立 Gateway 进程** |
| Web 路径 | **Web → Gateway → Portal**（Gateway 代理 SSE） |
| 会话绑定 | **`channel_id + peer_id` 自动建/续 session**（续聊键不含 `agent_id`） |
| 渠道配置权威 | **Gateway 自管**；Portal 只管 Agent / Session / 对话执行 |
| Webhook 回复 | **双模**：默认异步（202 + `reply_url`）；请求可声明同步；**Web 仍 SSE 流式** |
| Web 鉴权 | **在 Gateway 终止**；Portal Runtime 只认 service token + Gateway 断言的调用方身份 |
| 旧 Chat 入口 | **对外关闭**用户对话直连（Chat/SSE）；管理 API 保留；仅 `/runtime/v1` 供 Gateway |
| 架构方案 | **Gateway + Portal Runtime 契约**（非薄反向代理、非事件总线一期） |
| 非目标（一期） | 企微/微信/钉钉正式适配器；Gateway 管理台 CRUD UI；事件总线；Webhook 完整 HITL；迁现有 wecom/wxpusher 出站进 Gateway |

---

## 1. 问题

现状：

- Web 直连 Portal Chat/SSE；对话入口与渠道配置散落在 Portal。
- Portal 已有 Channel CRUD，以及 **wecom / wxpusher 出站**、粗粒度 webhook 中间件；**缺少完整的多渠道入站会话脊柱**。
- Harness S4「全量 IM Gateway」曾整项推迟；但「Web + 机器人都能聊」需要可落地的中间形态，而不是一次做齐 OpenClaw。

因此需要：

1. 对外统一入站面（协议差异收敛在 Gateway）；
2. 对内干净的 Portal Runtime 契约（执行与持久化仍在 Portal）；
3. 一期只证明 Web + 通用 Webhook，为后续 IM Adapter 留同一接口。

---

## 2. 架构

```text
┌──────────┐   ┌──────────────────────────┐   ┌─────────────────────────────┐
│  Web UI  │──▶│         Gateway          │──▶│  Portal (Runtime)           │
└──────────┘   │                          │   │  · ResolveSession(peer)     │
┌──────────┐   │  · ChannelRegistry       │   │  · RunTurn / StreamTurn     │
│ Webhook  │──▶│  · Adapters (web/hook)   │   │  · Agent / tools / memory   │
│ (generic)│   │  · SessionRouter         │◀──│  · SSE / final reply events │
└──────────┘   │  · RuntimeClient         │   └─────────────────────────────┘
               │  · ReplyDispatcher       │
┌──────────┐   │                          │
│ IM later │┄┄▶│  (same Adapter iface)    │
└──────────┘   └──────────────────────────┘
```

**边界**:

| 侧 | 职责 |
|----|------|
| **Gateway** | 渠道配置、协议适配、入站鉴权、`peer→session` 路由、回复形态（SSE / ack+async） |
| **Portal** | Agent 执行、会话/消息持久化、工具/记忆/HITL；**入站对话只信任 Gateway**（service token） |
| **管理 API** | Portal 既有管理面（Agent CRUD 等）保留 |
| **旧用户对话入口** | 现有对外 Chat/SSE **一期关闭或仅内网调试开关默认关**；生产路径只有 Gateway → `/runtime/v1` |

**原则**:

1. Gateway 不跑 ReAct / 不持有工具注册真相；只做协议与路由。
2. Session 权威在 Portal；Gateway 可短缓存 `channel+peer → session_id`。
3. 一期 Adapter 仅 `web` 与 `webhook`；IM 实现同一 `Adapter` 接口后挂。
4. 现有 Portal `channels` 表与 wecom 出站**一期保留不动**；入站配置不以该表为权威。

---

## 3. 组件

### 3.1 Gateway

| 组件 | 职责 |
|------|------|
| **ChannelRegistry** | 本地渠道配置：`channel_id`、type、secret、IP 白名单、`default_agent`、默认 reply 策略 |
| **Adapter** | `NormalizeInbound` → 标准入站事件；一期：`web`、`webhook` |
| **SessionRouter** | 调用 Portal `sessions/resolve`；维护短缓存 |
| **RuntimeClient** | 持有 Portal base URL + service token；调用 Runtime API |
| **ReplyDispatcher** | Web：SSE 透传；Webhook：202 + 后台完成后 POST `reply_url` |

**Adapter 流水线**（所有渠道共用）:

```text
NormalizeInbound → ResolveSession → RunTurn/StreamTurn → DeliverReply
```

### 3.2 Portal Runtime API（一期最小集）

路径前缀建议：`/runtime/v1`（仅内网 / service token）。

```text
POST /runtime/v1/sessions/resolve
  Request:  { channel_id, peer_id, agent_id?, title? }
  Response: { session_id, agent_id, created }
  语义:
    - 续聊键 = channel_id + peer_id（见 §3.3）
    - 已存在映射：返回既有 session；**忽略**请求里不同的 agent_id（不改绑、不新开）
    - 首次创建：agent_id = 请求值 ?? Channel 侧传入的 default_agent（Gateway 负责填好）

POST /runtime/v1/turns
  Request:  {
              session_id, content,
              reply_mode: "stream" | "final",
              channel_id, peer_id,
              correlation_id?, idempotency_key?
            }
  stream → SSE（事件语义尽量兼容现有 chat SSE，便于 Web 少改）
  final  → **一期固定**：Portal **同步**返回 JSON
            `{ correlation_id, status: "ok"|"failed", content?, error? }`
            Gateway 在拿到结果后再 POST `reply_url`（Portal 不做出站回调）

GET /runtime/v1/sessions/{id}
GET /runtime/v1/sessions/{id}/messages
  # Web UI 历史列表经 Gateway 代理
```

**鉴权**:

- Gateway ↔ Portal：内部 **service token**（Portal 拒绝匿名 Runtime 调用）。
- **Web 用户鉴权在 Gateway 终止**：Gateway 校验现有登录态后，向 Portal 只带 service token，并附带断言字段（如 `X-Sath-User-Id` / 等价 claim）；Portal Runtime **不**把浏览器 cookie 当信任根。
- Webhook：channel **secret**（及可选 IP 白名单）在 Gateway 校验。

### 3.3 SessionKey（续聊键）

Portal 持久化映射键（**不含 agent_id**）:

```text
channel:{channel_id}:peer:{peer_id}
```

- 同 key → 同一 `session_id`（续聊）；`agent_id` 是 session 创建时写入的属性，不是续聊键的一部分。
- 不同 `peer_id` 不得串会话。
- 若产品日后需要「同 peer 换 Agent 新开会话」，须显式 API（非一期）。

### 3.4 渠道配置（Gateway 权威）

一期可用配置文件或 Gateway 简易管理 API（**非**完整 Web 管理台）。字段至少包括：

- `channel_id`, `type` (`web` | `webhook`), `enabled`
- `default_agent`
- `webhook_secret`, `ip_whitelist`（webhook）
- `default_reply_mode`（webhook 默认 `async`）

Web 渠道的 `peer_id` 规则：登录用户 ID（或等价稳定身份）；匿名场景需显式策略（一期建议要求登录，与现网一致则沿用现有用户身份）。

---

## 4. 数据流

### 4.1 Web（stream）

1. Web → Gateway：发消息（及既有会话列表/历史读路径经代理）。
2. Gateway 鉴权终端用户。
3. `sessions/resolve`（peer 来自 Gateway 断言的用户身份）。
4. Portal `turns` + `reply_mode=stream`。
5. Gateway **透传 SSE** → Web。
6. 客户端取消 / 断连：取消 Portal 请求 ctx。

### 4.2 Webhook（默认 async）

1. `POST /hooks/{channel_id}`（或等价路径）。
2. 校验 secret / IP；channel disabled → 410。
3. Normalize：`content`, `peer_id`, 可选 `agent_id` / `reply_url` / `idempotency_key` / `reply_mode`。
4. `sessions/resolve`。
5. **立即** `202 Accepted` + `{ correlation_id }`。
6. 后台：Portal `turns` + `reply_mode=final`。
7. 成功：POST `reply_url` 最终文本（及 correlation_id）；无 `reply_url` 时记日志，并可提供状态查询（一期可选）。
8. 若请求显式 `reply_mode=sync`：Gateway 同步等待 final 后直接 HTTP 响应（须有超时上限）。

### 4.3 与现有 Portal Channel 的关系

| 能力 | 一期 |
|------|------|
| Portal Channel CRUD / wecom 出站 / cron 投递 | **保留** |
| 新入站 Web + Webhook | **走 Gateway** |
| 合并两套 Channel 配置 | **二期**再议 |

---

## 5. 错误处理与降级

| 情况 | 行为 |
|------|------|
| 鉴权失败（用户 / webhook secret） | Gateway 401/403，不打 Portal |
| 未知 / disabled channel | 404 / 410 |
| resolve 失败 | 502；可重试；不创建半吊子本地 session |
| Turn 超时 | 默认上限 **120s**（可配置）；Web：SSE error 事件后结束；Webhook 异步：向 `reply_url` 发 `status=failed`；sync Webhook 直接 HTTP 504/失败体 |
| HITL / confirm | **Web** 保持现有 SSE confirm；**Webhook** 无交互面时对需确认的危险操作 **fail-closed**（返回需在 Web 确认，或不自动执行） |
| 幂等 | Webhook 可选 `idempotency_key`；同 key **不重复开 turn**：返回原 `correlation_id`；若首次已完成且 Gateway 仍持有结果，可再次投递同一最终结果；进行中则返回同一 202 |
| Portal 不可达 | Gateway 5xx / 异步 failed 回调；不吞错 |

---

## 6. 测试与验收

### 6.1 测试分层

- **Gateway 单元**：Adapter normalize、SessionRouter 缓存、ReplyDispatcher（SSE 透传 / 202+reply_url）、鉴权与幂等。
- **Portal Runtime 契约**：同 peer resolve 同 session；stream/final 两种 turns；拒绝无 token。
- **集成**：mock Portal 测 Gateway；SSE 事件契约测试降低 Web 改动风险。
- **E2E 烟雾**：Web 经 Gateway 一轮流式对话；Webhook POST → 202 → `reply_url` 收到最终文本。

### 6.2 一期验收清单

1. 独立启动 Gateway + Portal；Web **只访问 Gateway** 仍可流式聊天。
2. 配置一条 generic webhook；签名错误拒绝；正确请求返回 202。
3. 同一 `channel_id + peer_id` 连续两轮落在同一 Portal session。
4. 不同 peer 不串 session。
5. 异步失败时 `reply_url` 收到 failed（或等价状态）。
6. 现有 Portal Channel / wecom 出站行为不被破坏。

### 6.3 非目标（一期）

- 企微 / 微信 / 钉钉 / 飞书正式入站适配器
- Gateway 完整管理台 UI
- 事件总线、多副本复杂亲和调度
- Webhook 完整 HITL 交互
- 将 wecom/wxpusher 出站迁入 Gateway

---

## 7. 仓库与部署草图

- 建议 monorepo 新增 `gateway/`（独立 Go module / 二进制），与 `portal/`、`web/` 并列。
- Compose：增加 `gateway` 服务；Web 开发代理从 Portal 改为 Gateway；Portal Runtime 端口仅内网可达（或同 compose 网络）。
- 配置：Gateway 持有 Portal URL + service token；渠道配置在 Gateway 侧。

---

## 8. 后续（非一期）

1. 企微应用 / 群机器人**入站** Adapter（与现有出站并存或逐步统一配置）。
2. 微信等第三方 bot Adapter。
3. Channel 配置统一（Gateway 权威 vs Portal 出站）与管理 UI。
4. 若运维需要：Gateway 水平扩展 + session 亲和或无状态（仅缓存）。
5. Webhook HITL：确认卡映射为 IM 按钮/二次消息（单独设计）。

---

## 9. 开放问题（实现计划阶段收敛）

1. Web 现有 session 列表/创建/历史 API 经 Gateway 的代理切面清单（最小改动集）。
2. Service token 轮换与本地开发默认凭据。
3. Web `peer_id` 取值：稳定用户 ID vs 允许「每浏览器一会话」的派生规则（须仍满足同 peer 续聊验收）。
