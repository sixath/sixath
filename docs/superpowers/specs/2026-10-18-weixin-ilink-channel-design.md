# 微信 iLink 机器人渠道（`weixin_ilink`）

**日期**: 2026-10-18  
**状态**: 设计已确认，待写实现计划  
**方案**: Gateway 负责 iLink 长轮询入站与对话回复；Portal 负责扫码登录、渠道/名册真相源与出站推送（Cron / `send_to_weixin`）  
**关联**:
- [`2026-08-09-inbound-gateway-design.md`](./2026-08-09-inbound-gateway-design.md)（入站 Gateway 契约）
- [`2026-08-09-wecom-bot-gateway-design.md`](./2026-08-09-wecom-bot-gateway-design.md)（长连接 Adapter，本设计的对照模板）
- [`2026-08-10-gateway-portal-agent-routing-design.md`](./2026-08-10-gateway-portal-agent-routing-design.md)（default/白名单 Agent、slash 指令）
- [`2026-08-10-gateway-message-auto-route-design.md`](./2026-08-10-gateway-message-auto-route-design.md)（`@Agent` + 分类器）
- [`2026-08-11-channel-portal-runtime-status-design.md`](./2026-08-11-channel-portal-runtime-status-design.md)（渠道配置同步、Runtime Status）
- 协议参考：腾讯官方 OpenClaw 插件 [`Tencent/openclaw-weixin`](https://github.com/Tencent/openclaw-weixin)（`src/api/api.ts`）；社区整理 [`epiral/weixin-bot` protocol-spec](https://github.com/epiral/weixin-bot/blob/main/docs/protocol-spec.md)

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 能力范围 | **入站对话** + **出站推送**（Cron 投递、Agent 工具） |
| 账号模型 | **一个 Portal 渠道 = 一个 iLink bot 账号**；管理员在 Portal 扫码登录 |
| 访问控制 | **对端白名单**；首次发言自动入名册（默认拒绝），管理员在 Portal 放行；扫码人自动放行 |
| 推送目标 | **两种都支持**：推回发起人（会话反查自动回填）+ Portal 从名册手选 |
| 架构 | **方案 A**：Gateway 收 + 回复；Portal 登录 + 出站直发 `sendmessage` |
| 协议代码 | Gateway / Portal 两个 Go 模块**各放一份精简客户端**，共用 JSON fixture 测试 |
| 非目标（一期） | 图片/文件收发、群聊、微信内 HITL、`sendtyping`、多副本分布式租约、按 sixath 用户身份跑 Agent |

---

## 1. 问题

- sixath 已有企微入站（`wecom_bot` 长连接）和推送（`wecom` 群 Webhook、`wxpusher`），但**没有个人微信的双向对话入口**。
- 腾讯通过 iLink 机器人网关（微信 ClawBot 背后的 HTTP/JSON 协议）开放了"扫码授权 → 长轮询收消息 → 回消息"的能力，可以用来做 sixath 与微信的互通。
- iLink 的协议形态与企微长连接不同（HTTP 长轮询、每条回复必须带 `context_token`、会话会过期需重扫），不能直接复用 `wecom_bot` adapter，需要新的渠道类型。

---

## 2. 目标与非目标

### 2.1 目标

1. 管理员在 Portal Channels 页创建 `type=weixin_ilink` 渠道，页面内扫码完成登录。
2. 白名单内的微信用户私聊 bot → 按渠道 default / 绑定 Agent 跑 Turn → 纯文本回复。
3. 现有入站能力全部可用：`/switch`、`/who`、`/agent`、`/new`、`/unbind`、`@Agent` 自动路由、消息幂等。
4. Cron 结果可推送到指定微信对端；在微信里创建的 Cron 默认推回发起人。
5. Agent 可通过 `send_to_weixin` 工具主动给名册内已放行的对端发消息。
6. 会话过期（`-14`）在 Portal 可见（`login_required`），重新扫码后自动恢复。

### 2.2 非目标

- 图片、文件、语音的收发（`getuploadurl` + CDN AES 上传）。非文本入站统一回复"暂只支持文字消息"。
- 群聊（协议有 `group_id` 字段，但公开实现主要是单聊）。
- 在微信内完成 HITL 确认：fail-closed，提示去 Web。
- 输入中提示（`sendtyping` / `getconfig` 的 `typing_ticket`）。
- 多 Gateway 副本的分布式 leader 租约：沿用 `leadership.AlwaysLeader`，单副本约束同 `wecom_bot`。
- 把微信用户绑定到 sixath 用户、按其身份/权限跑 Agent。

---

## 3. iLink 协议要点（实现依据）

| 项 | 内容 |
|----|------|
| 基址 | `https://ilinkai.weixin.qq.com`（可配置） |
| 通用请求头 | `Content-Type: application/json`、`AuthorizationType: ilink_bot_token`、`Authorization: Bearer {bot_token}`、`X-WECHAT-UIN: base64(随机 uint32 十进制串)` |
| 通用 body | `base_info: { channel_version }` |
| 扫码登录 | `GET /ilink/bot/get_bot_qrcode?bot_type=3` → 二维码；`GET /ilink/bot/get_qrcode_status?qrcode=…` → `wait` / `scaned` / `confirmed` / `expired`；`confirmed` 时返回 `bot_token`、`ilink_bot_id`、`ilink_user_id`（扫码人）、可选 `baseurl` |
| 收消息 | `POST /ilink/bot/getupdates`，body `{ get_updates_buf }`（首次 `""`）；服务端挂起至有消息或约 35s；响应 `msgs[]` + 新 `get_updates_buf` + `longpolling_timeout_ms` |
| 消息字段 | `message_id`、`from_user_id`（`…@im.wechat`）、`to_user_id`（`…@im.bot`）、`message_type`（1=用户，2=bot）、`context_token`、`item_list[]`（`type=1` → `text_item.text`） |
| 发消息 | `POST /ilink/bot/sendmessage`，body `msg: { from_user_id:"", to_user_id, client_id, message_type:2, message_state:2, context_token, item_list:[{type:1,text_item:{text}}] }`；成功通常为空对象 |
| `context_token` | **回复必需**；缺失时服务端返回 200 但消息不投递。只能由对端先发消息获得；按 `(channel, peer)` 缓存最近一次 |
| 会话过期 | `ret` 或 `errcode` = `-14`（`session timeout`）→ 停止收发、清游标、重新扫码 |

客户端侧超时（长轮询无响应）视为空批次，直接进入下一轮，不算错误。

---

## 4. 架构

```text
┌────────────┐  getupdates 长轮询 / sendmessage    ┌─────────────────────────────┐
│ 微信 iLink │◀───────────────────────────────────▶│ Gateway                     │
│ ilinkai.   │                                     │  WeixinIlinkManager         │
│ weixin.qq  │                                     │   (每渠道一个长轮询 runner)   │
│  .com      │                                     │  → 名册上报 → 幂等 → 白名单  │
└─────┬──────┘                                     │  → slash / 自动路由 → Turn  │
      │                                            └──────────┬──────────────────┘
      │ get_bot_qrcode / get_qrcode_status                    │ /runtime/v1
      │ sendmessage（出站推送）                                ▼ 上报 context_token / 游标 / 状态
      │                                            ┌─────────────────────────────┐
      └───────────────────────────────────────────▶│ Portal                      │
                                                   │  扫码登录 · 渠道 / 名册 CRUD │
                                                   │  Cron 投递 · send_to_weixin │
                                                   └─────────────────────────────┘
```

### 4.1 职责边界

| 组件 | 做什么 | 不做什么 |
|------|--------|----------|
| **Gateway** | iLink 长轮询、入站归一化、名册上报、白名单拦截、复用现有 slash/自动路由/Resolve/Turn 流水线、`sendmessage` 回复、Runtime Status 上报 | 不持有 token/白名单真相；不做扫码登录；不做出站推送 |
| **Portal** | 渠道 CRUD 与扫码登录、名册（白名单 + `context_token`）与游标持久化、Cron `weixin_ilink` 投递、`send_to_weixin` 工具 | 不跑长轮询 |

数据流方向保持不变：**只有 Gateway → Portal**，Portal 不调用 Gateway。Gateway 停机不影响 Cron 推送（只要 `context_token` 仍有效）。

### 4.2 组件清单

**Gateway**

- `internal/ilink/`：精简协议客户端（请求头、`GetUpdates`、`SendText`、`-14` 识别）+ 入站归一化（取文本、判非文本）+ 文本整形（Markdown 简化、按约 1800 字切分）。
- `internal/adapter/weixin_ilink_manager.go`：对齐 `WecomBotManager`，`Reconcile(prev, next)` 按 Portal 同步结果启停/重启 runner；`ConnectionConfigChanged` 覆盖 `bot_token` / `base_url` 变化；仅 leader 运行。
- `internal/adapter/weixin_ilink.go`：runner 循环与单条消息处理（见 §6.2）。
- `internal/runtimeclient`：新增名册上报、游标读写接口的客户端方法。
- `internal/channel.Channel`：新增 `BotToken`、`BaseURL`、`AllowedPeers` 字段。

**Portal**

- `internal/ilink/`：精简协议客户端（扫码登录两接口 + `SendText`）。
- `internal/channel/`：`PushToWeixinIlink(ctx, ch, peerID, text)`。
- 渠道 service：扫码登录接口、名册接口（列表 / 放行 / 拒绝 / 备注）。
- `/runtime/v1`：名册上报、游标读写接口；`gateway/channels` 下发 iLink 字段。
- Cron executor：`deliverChannel` 新增 `weixin_ilink` 分支；`cronjob` 工具的"推回发起人"回填。
- `chat`：`send_to_weixin` 工具注册，通过 `ChannelCatalogProvider` 绑定（`channelTypeTools["weixin_ilink"] = {"send_to_weixin"}`）。
- Web：Channels 表单（扫码弹窗、名册 Tab）、Cron 表单（对端下拉）。

---

## 5. 数据模型

迁移：`portal/migrations/023_weixin_ilink_channel.sql`（编号以实现时最新为准）。

### 5.1 `channels` 新增列（仅 `type=weixin_ilink` 使用）

| 列 | 类型 | 说明 |
|----|------|------|
| `ilink_bot_token` | varchar(512) | 扫码所得凭证。**API 不回显明文**，对外只给 `has_bot_token`（同 Agent `has_api_key`） |
| `ilink_bot_id` | varchar(128) | bot 账号 ID（`…@im.bot`），展示/校验用 |
| `ilink_owner_user_id` | varchar(128) | 扫码人微信 ID（`…@im.wechat`） |
| `ilink_base_url` | varchar(512) | 可选；空 = `https://ilinkai.weixin.qq.com`；登录返回 `baseurl` 时写入 |

`default_agent`、`allowed_agents`、`auto_route_*`、`enabled` 直接复用。`Type` 注释补上 `weixin_ilink`。

### 5.2 新表 `channel_ilink_peers`（对端名册，白名单唯一真相）

| 列 | 类型 | 说明 |
|----|------|------|
| `channel_id` | varchar(64) PK | |
| `peer_id` | varchar(128) PK | 微信用户 ID |
| `allowed` | bool, default false | 白名单标志 |
| `remark` | varchar(128) | 管理员备注名（iLink 不提供昵称） |
| `context_token` | varchar(512) | 最近一次入站消息的 token；**API 不回显**，只给 `has_context_token` |
| `context_token_at` | datetime null | token 获取时间 |
| `last_inbound_at` | datetime null | 最近发言时间 |
| `last_preview` | varchar(160) | 最近发言前 40 字，帮管理员识别 |
| `created_at` / `updated_at` | datetime | |

为什么白名单放名册而不是 `channels` 列：微信 ID 是不透明串，管理员事先不知道；必须先"看到"对端再放行，名册天然承载这一流程。

### 5.3 新表 `channel_ilink_state`（长轮询运行时状态）

| 列 | 类型 | 说明 |
|----|------|------|
| `channel_id` | varchar(64) PK | |
| `get_updates_buf` | text | 游标；重扫登录 / `-14` 时清空 |
| `updated_at` | datetime | |

不放进 `channels`，避免游标高频变化干扰 Gateway 配置 diff 与 `updated_at` 语义。

### 5.4 `cron_tasks` 新增列

| 列 | 说明 |
|----|------|
| `delivery_peer_id` varchar(128) | `delivery_mode=channel` 且渠道为 `weixin_ilink` 时**必填** |

### 5.5 Runtime Status

复用 `channel_runtime_status`；`weixin_ilink` 的状态集：`connected` / `reconnecting` / `login_required` / `disabled`，读路径派生 `unknown`（无心跳或超过 90s）。`login_required` 时 `last_error` 写原因（未登录 / 会话过期）。

无 `bot_token` 的渠道：Gateway 不启动 runner，Portal 读路径直接派生 `login_required`。

### 5.6 Gateway 同步下发

`GET /runtime/v1/gateway/channels` 对 `weixin_ilink` 额外返回：

```json
{
  "bot_token": "…",
  "base_url": "https://ilinkai.weixin.qq.com",
  "allowed_peers": ["o9cq…@im.wechat"]
}
```

`allowed_peers` 由名册 `allowed=true` 派生。该接口本就以 service token 下发 `wecom_bot` secret，安全级别一致。白名单改动最长约 15s（一个同步周期）生效。

---

## 6. 流程

### 6.1 扫码登录（Portal，仅管理员）

1. 管理员创建 `weixin_ilink` 渠道 → 无 token，状态 `login_required`。
2. 点"扫码登录" → `POST /api/v1/channels/{id}/ilink/login` → Portal 调 `get_bot_qrcode` → 返回 `{ qrcode, qrcode_img_content }`，前端渲染二维码。
3. 前端每 2s 轮询 `GET /api/v1/channels/{id}/ilink/login/{qrcode}` → Portal 调 `get_qrcode_status`：
   - `wait` / `scaned`：原样返回，前端提示"等待扫码 / 已扫码，请在手机确认"。
   - `expired`：前端提示并允许"刷新二维码"（重新走第 2 步）。
   - `confirmed`：**单事务**写入 `ilink_bot_token` / `ilink_bot_id` / `ilink_owner_user_id` / `ilink_base_url`，清空 `channel_ilink_state.get_updates_buf`，upsert owner 到名册并 `allowed=true`。返回 `{ status:"confirmed", bot_id }`。
4. 渠道配置变化 → Gateway 下一次同步时 `ConnectionConfigChanged=true` → 重启 runner → `connected`。

二维码状态不落库：每次轮询直接透传 iLink 结果，无需 Portal 端会话。

### 6.2 入站对话（Gateway runner）

```text
启动 → GET 游标
loop:
  getupdates(buf) ──超时──▶ 空批次，继续
       │ -14 ──▶ §7 会话过期
       │ 网络/5xx ──▶ 退避重试（reconnecting）
       ▼
  for msg in msgs where message_type == 1:
     ① 上报名册：context_token / last_inbound_at / last_preview   （在白名单之前）
     ② 幂等：ilink:{channel_id}:{message_id} 已处理 → 跳过
     ③ 白名单：peer ∉ allowed_peers → 回"无权限，请联系管理员开通（ID 尾号 xxxx）"，结束
     ④ 非文本 → 回"暂只支持文字消息"，结束
     ⑤ 投递到该 peer 的串行队列
  提交新游标
```

单条消息在 peer 队列中的处理（复用现有流水线，peer 键 `user:{from_user_id}`）：

1. pending `/switch` 序号 → slash 指令 → `prepareAutoRoute`（`@Agent` / 分类器）→ Resolve → `TurnsFinal(reply_mode=final)`，超时沿用 600s。
2. 15s 内 Turn 未结束 → 先 `sendmessage` 一条"处理中…"（每条入站最多一次）。
3. 回复：Markdown 简化为纯文本（去标题/加粗标记、代码块保留内容）→ 按约 1800 字切分 → 依次 `sendmessage`，均携带本条消息的 `context_token`。
4. 只回助手正文，不含工具时间线（同 `wecom_bot`）。

并发模型：**同一 peer 串行，不同 peer 并发**；长 Turn 不阻塞长轮询与其他对端。游标在批次分派后提交；进程崩溃时在途 Turn 丢失，与 `wecom_bot` 语义一致，重复投递由幂等兜底。

### 6.3 出站推送（Portal）

统一入口：

```go
func PushToWeixinIlink(ctx context.Context, ch *biz.Channel, peerID, text string) error
```

1. 查名册：不存在或 `allowed=false` → `目标 {peer} 未在白名单`；`context_token` 为空 → `对方尚未与 bot 对话，无法主动推送`。均**不调用** iLink。
2. 文本整形与切分同 §6.2，逐条 `sendmessage`。
3. `-14` → 返回错误，并把该渠道 Runtime Status 写为 `login_required`。

**Cron 投递**

- `deliverChannel` 新增 `case "weixin_ilink"`：要求 `delivery_peer_id`，调用 `PushToWeixinIlink`；错误信息更新"仅支持 …"列表。
- **Portal 手选**：Cron 表单选中 `weixin_ilink` 渠道后显示对端下拉（名册中 `allowed=true`，显示 `remark` 或 ID 尾号）。
- **推回发起人**：`cronjob` 工具在 create 时若未指定投递方式，且当前会话可经 `channel_peer_sessions` 反查到 `weixin_ilink` 渠道 + peer，则自动回填 `delivery_mode=channel`、`delivery_channel_id`、`delivery_peer_id`。显式指定的投递参数优先。`cronjob` 工具新增可选参数 `delivery_peer_id`。

**`send_to_weixin` 工具**

| 参数 | 必填 | 说明 |
|------|------|------|
| `text` | 是 | 发送内容 |
| `peer_id` | 否 | 省略时：当前会话来自 `weixin_ilink` → 发给当前对端；否则报错要求指定 |

- 渠道绑定：`ChannelCatalogProvider` 注入 `channel_id` / `channel_type`，检索词 `微信`、`weixin`、`推送`。
- 复用 `DeliveryRecorder` 记录投递，复用按会话频率限制（同 `send_to_wecom`）。
- `peer_id` 必须在名册且已放行，校验在 `PushToWeixinIlink` 内统一完成。

---

## 7. 错误处理

| 场景 | 处理 |
|------|------|
| `getupdates` 网络错误 / 5xx | 指数退避 1s → 60s 封顶；上报 `reconnecting`（附 attempt / 下次间隔）；恢复后 `connected` |
| `-14` 会话过期（入站） | 立即停止该渠道收发；清空游标；上报 `login_required`（`last_error=会话过期，请重新扫码`）；**不自动重试**，直到 `bot_token` 变化触发重启 |
| 名册上报失败 | 记日志，不阻塞回复 |
| 游标提交失败 | 保留在内存，下批一并提交；重复消息由幂等兜底 |
| Turn 超时 / 出错 | 回"处理失败：{简述}"，不静默丢弃 |
| 回复需要 HITL | fail-closed："该操作需要在 Web 端确认" |
| 出站 `-14` | 返回错误（Cron 记入 `delivery_error`）；渠道状态写 `login_required` |
| 出站无 token / 不在白名单 | 明确报错，不调 iLink |
| 日志脱敏 | `bot_token`、`context_token` 一律不打日志；Portal API 只给 `has_*` |

---

## 8. 测试

**协议层（Gateway、Portal 各一份）**

- `httptest` 模拟 iLink：请求头齐全且 `X-WECHAT-UIN` 为合法 base64；`getupdates` 游标推进；客户端超时 → 空批次；`-14`（`ret` 与 `errcode` 两种）识别；`sendmessage` body 字段。
- 两模块共用同一组 JSON fixture（`testdata/ilink/*.json`，按模块各拷一份，CI 校验内容一致），保证两份实现行为一致。

**Gateway**

- Manager `Reconcile`：启用/停用、`bot_token` 变化重启、无 token 不启动、非 leader 不启动。
- 入站：名册上报先于白名单；白名单拒绝不跑 Turn；幂等去重；非文本提示；slash 与 `/switch` 两步；"处理中"计时器（Turn < 15s 不发、> 15s 发一次）；长文本切分；同 peer 串行 / 跨 peer 并发。
- `-14` → 停止并上报 `login_required`；网络错误退避。

**Portal**

- 扫码状态机 `wait` / `scaned` / `confirmed` / `expired`；`confirmed` 单事务（token、owner 放行、游标清空）。
- 名册上报 upsert；放行/拒绝/备注接口；`gateway/channels` 下发 `allowed_peers`；API 不回显 token。
- `deliverChannel` `weixin_ilink` 分支（缺 peer / 不在白名单 / 无 token / 成功 / `-14`）。
- `cronjob` 推回发起人回填（有反查 / 无反查 / 显式参数优先）。
- `send_to_weixin` 参数默认值与校验。
- 迁移测试。

**端到端验收（写入 Gateway README）**

1. Portal 创建渠道 → 扫码 → 约 15s 内状态 `connected`；owner 已在名册且放行。
2. 非白名单微信号发消息 → 收到"无权限"；名册出现该对端与预览。
3. 名册放行 → ≤15s 后对话正常；`/who`、`/switch` 可用。
4. 微信内让 Agent 建 Cron → 到点推回本人。
5. Portal 手动建 Cron 选对端 → 推送成功；选未对话过的对端 → `delivery_error` 明确。
6. `send_to_weixin` 在 Web 会话中指定 `peer_id` 推送成功。
7. 模拟/等待会话过期 → `login_required` → 重扫后恢复。
8. 停 Gateway → ≤90s 显示 `unknown`；Cron 推送仍可用。

---

## 9. 风险与开放问题

| 项 | 说明 / 缓解 |
|----|-------------|
| 协议非正式开放 | iLink 目前以官方 OpenClaw 插件形态交付，HTTP 协议由源码公开；接口可能变动。协议常量集中在 `internal/ilink`，`channel_version` 可配置 |
| `context_token` 有效期未知 | 公开资料未给出 TTL。出站失败（投递不到）无法从 200 响应判断；记录 `context_token_at` 供排查，后续视实测补"过旧 token 告警" |
| 单副本约束 | 同一 bot 多实例同时长轮询会重复收消息；与 `wecom_bot` 相同，文档明确单副本 |
| 消息长度上限 | 1800 字切分为保守值，实测后调整为配置项 |
