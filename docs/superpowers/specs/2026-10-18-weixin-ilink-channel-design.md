# 微信 iLink 机器人渠道（`weixin_ilink`）

**日期**: 2026-10-18  
**状态**: 设计已确认（rev3：Phase 0 实测结论已回写，见 §10），待写第 1～3 份实现计划  
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
| 账号模型 | **一个 Portal 渠道 = 一个 iLink bot 账号**；在 Portal 渠道页扫码登录 |
| 访问控制 | **对端白名单**（名册表）；首次发言自动入名册、默认拒绝，在 Portal 放行；扫码人自动放行 |
| 推送目标 | **两种都支持**：推回发起人（会话反查自动回填）+ Portal 从名册手选 |
| 架构 | **方案 A**：Gateway 收 + 回复；Portal 登录 + 出站直发 `sendmessage` |
| Turn 调用 | Gateway 用 **`TurnsStream`**（同 `wecom_bot`），不受 Portal HTTP 120s 超时限制 |
| 投递语义 | **至多一次**：游标在处理前提交；进程崩溃时在途 Turn 丢失（与 `wecom_bot` 一致） |
| 新表外键 | 一律用 **`channels.id`（UUID，不可变）**；Runtime API 收 slug，服务端换算 |
| 权限 | 扫码/名册接口与现有 Channel 管理接口**一致（登录即可）**；本期不引入管理员角色 |
| 协议代码 | Gateway / Portal 两个 Go 模块**各放一份精简 HTTP 客户端**，共用同一组 JSON fixture（人工同步） |
| 非目标（一期） | 图片/文件收发、群聊、微信内 HITL、`sendtyping`、多副本 leader 租约、按 sixath 用户身份跑 Agent、管理员 RBAC |

---

## 1. 问题

- sixath 已有企微入站（`wecom_bot` 长连接）和推送（`wecom` 群 Webhook、`wxpusher`），但**没有个人微信的双向对话入口**。
- 腾讯通过 iLink 机器人网关（微信 ClawBot 背后的 HTTP/JSON 协议）开放了"扫码授权 → 长轮询收消息 → 回消息"的能力，可用于 sixath 与微信互通。
- iLink 形态与企微长连接不同（HTTP 长轮询、回复必须带 `context_token`、会话会过期需重扫），不能直接复用 `wecom_bot` adapter，需要新的渠道类型。

---

## 2. 目标与非目标

### 2.1 目标

1. 在 Portal Channels 页创建 `type=weixin_ilink` 渠道，页面内扫码完成登录。
2. 白名单内的微信用户私聊 bot → 按渠道 default / 绑定 Agent 跑 Turn → 纯文本回复。
3. 复用现有入站能力：`/switch`、`/who`、`/agent`、`/new`、`/unbind`、`@Agent` 自动路由、进程内消息幂等。
4. Cron 结果可推送到指定微信对端；在微信里创建的 Cron 默认推回发起人。
5. Agent 可通过 `send_to_weixin` 工具主动给名册内已放行的对端发消息。
6. 会话过期（`-14`）在 Portal 持续可见为 `login_required`，重新扫码后自动恢复。

### 2.2 非目标

- 图片、文件、语音收发（`getuploadurl` + CDN AES 上传）。非文本入站统一回复"暂只支持文字消息"。
- 群聊（协议有 `group_id`，公开实现主要是单聊）。
- 微信内 HITL 确认：fail-closed，提示去 Web。
- 输入中提示（`sendtyping` / `getconfig`）。
- 多 Gateway 副本 leader 租约：与 `wecom_bot` 相同，**单副本约束**写入文档，不接 `leadership`。
- 微信用户绑定 sixath 用户。
- 管理员角色 / Channel ACL（现有 Channel 接口也没有，单独立项）。

---

## 3. iLink 协议要点（实现依据）

| 项 | 内容 |
|----|------|
| 基址 | 默认 `https://ilinkai.weixin.qq.com`；渠道级可覆盖（`ilink_base_url`，登录返回 `baseurl` 时写入）。扫码前一律用默认基址 |
| 通用请求头 | `Content-Type: application/json`、`AuthorizationType: ilink_bot_token`、`Authorization: Bearer {bot_token}`、`X-WECHAT-UIN: base64(随机 uint32 十进制串)` |
| 通用 body | `base_info: { channel_version }`；`channel_version` 为代码常量（两模块各一份） |
| 扫码登录 | `GET /ilink/bot/get_bot_qrcode?bot_type=3` → 二维码；`GET /ilink/bot/get_qrcode_status?qrcode=…` → `wait` / `scaned` / `confirmed` / `expired`；`confirmed` 返回 `bot_token`、`ilink_bot_id`、`ilink_user_id`（扫码人）、可选 `baseurl`。**实测为服务端挂起的长轮询**：无事件时约 30s 返回 `wait`；确认事件会立即唤醒挂起中的请求；实测未出现 `scaned`（见 §10）。**每次扫码（含同一微信号重扫）都会得到新的 `ilink_bot_id`，旧 `bot_token` 立即失效** |
| 收消息 | `POST /ilink/bot/getupdates`，body `{ get_updates_buf }`（首次 `""`）；服务端挂起至有消息或约 35s；响应 `msgs[]` + 新 `get_updates_buf` |
| 消息字段 | `message_id`、`from_user_id`（`…@im.wechat`）、`message_type`（1=用户，2=bot）、`context_token`、`item_list[]`（`type=1` → `text_item.text`） |
| 发消息 | `POST /ilink/bot/sendmessage`，`msg: { from_user_id:"", to_user_id, client_id, message_type:2, message_state:2, context_token, item_list:[{type:1,text_item:{text}}] }`；成功响应带 `message_id`（实测），可写入投递记录 |
| `context_token` | **回复必需**；缺失或已失效时服务端都返回 200 但不投递（静默失败）。只能由对端先发消息获得；按 `(channel, peer)` 缓存最近一次。**绑定在 bot 上**：重扫换 bot 后旧 token 不可用（实测）。同一 token 5 分钟内连续发 9 条均送达 |
| 会话过期 | `ret` 或 `errcode` = `-14` → 停止收发、清游标、重新扫码。实测旧 bot 返回 `{"errcode":-14,"errmsg":"session timeout"}`（`ret` 缺省为 0），两个字段都要判断 |
| 自身消息 | bot 发出的消息不会出现在 `getupdates` 中（实测）；仍按 `message_type == 1` 过滤以防万一 |

客户端侧超时（长轮询无响应）视为空批次。

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
      │ sendmessage（出站推送）                                ▼ 名册 / 游标 / 会话失效 / 心跳
      │                                            ┌─────────────────────────────┐
      └───────────────────────────────────────────▶│ Portal                      │
                                                   │  扫码登录 · 渠道 / 名册      │
                                                   │  Cron 投递 · send_to_weixin │
                                                   └─────────────────────────────┘
```

### 4.1 职责边界

| 组件 | 做什么 | 不做什么 |
|------|--------|----------|
| **Gateway** | iLink 长轮询、入站归一化、名册上报、白名单拦截、复用 slash/自动路由/Resolve/`TurnsStream` 流水线、`sendmessage` 回复、心跳与会话失效上报 | **不是** token / 白名单的真相源（只用同步下发的副本）；不做扫码；不做出站推送 |
| **Portal** | 渠道 CRUD 与扫码登录、名册与游标持久化、会话失效状态、Cron `weixin_ilink` 投递、`send_to_weixin` 工具 | 不跑长轮询 |

数据流方向不变：**只有 Gateway → Portal**。Gateway 停机不影响 Cron 推送（只要 `context_token` 仍有效）。

### 4.2 改动清单

**Gateway（`gateway/`）**

| 位置 | 改动 |
|------|------|
| `internal/ilink/`（新） | 精简协议客户端：请求头、`GetUpdates`、`SendText`、`-14` 识别；入站归一化（取文本 / 判非文本）；文本整形（Markdown 简化、按 `split_runes` 切分，默认 4000 字）。只做 HTTP，不依赖 runtimeclient |
| `internal/channel/registry.go` | `Channel` 新增 `BotToken`、`BaseURL`、`Generation`、`SessionExpired`、`AllowedPeers` |
| `internal/runtimeclient/client.go` | `gatewayChannelItem` 映射新字段；新增 `ReportIlinkPeer`、`GetIlinkCursor`、`PutIlinkCursor`、`ReportIlinkSessionExpired` |
| `internal/adapter/weixin_ilink_manager.go`（新） | 对齐 `WecomBotManager` 的 `Reconcile` / start / stop / drain。**可运行条件** = `enabled && BotToken != "" && !SessionExpired`。新增独立的 `IlinkConnectionChanged(a, b)`：比较 `BotToken`、`BaseURL`、`Generation`；**不含 `AllowedPeers`**（改白名单不重启 runner）。不改动现有 `ConnectionConfigChanged` |
| `internal/adapter/weixin_ilink.go`（新） | runner 循环、心跳、peer 队列、单条消息处理（§6.2） |
| `internal/channelsync/sync.go` | `Config.Manager` 改为可组合多个 `Reconciler`（或新增 `MultiReconciler`），同时驱动 `WecomBotManager` 与 `WeixinIlinkManager` |
| `cmd/gateway/main.go` | 装配新 manager |
| `README.md` | 渠道一览、单副本约束、验收清单 |

**Portal（`portal/`）**

| 位置 | 改动 |
|------|------|
| `internal/ilink/`（新） | 精简协议客户端：`GetBotQRCode`、`GetQRCodeStatus`、`SendText`、`-14` 识别。只做 HTTP |
| `internal/data/model/` | `Channel` 新列（§5.1）；新模型 `ChannelIlinkPeer`、`ChannelIlinkState`；`CronTask.DeliveryPeerID` |
| `internal/data/data.go` | 新模型加入 `AutoMigrate` 列表与 `ensureUTF8MB4` |
| `migrations/023_weixin_ilink_channel.sql` | 手工补充迁移（编号以实现时最新为准） |
| `internal/data/channel_mysql.go` | `ListGatewayChannels` 类型过滤加入 `weixin_ilink`；`Update` 可改字段白名单**不得**包含 `ilink_bot_token` / `ilink_generation` / `ilink_session_expired_at`（只由扫码与失效流程写）；新增 `ListIlinkByAgent(agentID)`（default 或 allowed 包含该 Agent、且 enabled） |
| `internal/data/channel_peer_mysql.go` | 新增 `GetBySessionID(sessionID)`（`session_id` 非唯一索引；多行时取 `updated_at` 最新） |
| `internal/data/channel_ilink_mysql.go`（新） | 名册与状态表 repo |
| `internal/biz/channel.go` + `data/channel_mysql.go` `channelModelToBiz` | `ChannelMeta` 新增 `HasIlinkBotToken`、`IlinkBotID`、`IlinkOwnerUserID`、`IlinkBaseURL`、`IlinkGeneration`、`IlinkSessionExpiredAt`（token 明文只在 usecase 内部取用） |
| `internal/biz/channel_runtime.go` | `DeriveRuntimeStatus` 支持 `weixin_ilink`（§5.5 优先级） |
| `internal/biz/identity.go` + `data/auth_ephemeral_mysql.go` | `AuthEphemeralRepo` 新增非消费读 `Get(ctx, id, kind, now)` |
| `internal/biz/weixin_ilink.go`（新） | `WeixinIlinkUsecase`：扫码登录编排、名册上报 / 放行 / 备注、游标 CAS、会话失效、`Push(ctx, channelUUID, peerID, text)` |
| `internal/biz/channel_usecase.go` | 删除渠道时级联清理名册与状态表 |
| `internal/biz/cron.go` + `api/cron/v1/cron.proto` + `cron_service` | create / update / reply 加 `delivery_peer_id`；create/update 时校验（§6.3）；`CronUsecase` 注入名册 repo，同步改 wire 装配 |
| `api/channel/v1/channel.proto` + `service/channel.go` | 回复加 `has_bot_token`、`ilink_bot_id`、`ilink_owner_user_id`（登录态由既有 `runtime_status` 表达，不另加字段）；新增扫码与名册接口（§6.1、§6.4） |
| `internal/runtime/` + `server/http.go` | Runtime API（§5.6） |
| `internal/cron/executor.go` | `deliverChannel` 新增 `weixin_ilink` 分支，调 `WeixinIlinkUsecase.Push` |
| 推回发起人回填 | 在 Portal 的 `portalCronClient.Create`（framework `CronJobClient` 的 Portal 实现）里做，见 §6.3 |
| `internal/chat/` + `service/wecom_bind.go` 同类位置 | 注册 `send_to_weixin`；`CatalogWiringInput` 支持多个渠道 provider（不再只有单个 `WecomChannelID` 槽位）；`channelTypeTools["weixin_ilink"] = {"send_to_weixin"}` |

**Framework（`framework/`）**

| 位置 | 改动 |
|------|------|
| `tool/cronjob_tool.go` | `CronJobCreateInput` / 工具 schema 新增可选 `delivery_peer_id`；**未指定 `delivery_mode` 时保持空串**，不再在工具层默认 `"none"`（由 Portal 实现兜底）。framework 不含任何微信逻辑 |

**Web（`web/`）**

| 位置 | 改动 |
|------|------|
| `pages/ChannelForm.tsx` | `weixin_ilink` 类型表单；"扫码登录"弹窗；名册 Tab（放行 / 拒绝 / 备注、最近预览、ID 尾号） |
| `pages/ChannelList.tsx` | `RUNTIME_DOT_COLORS` 增加 `login_required`；类型列 |
| `pages/CronTaskForm.tsx` | 选中 `weixin_ilink` 渠道时显示对端下拉 |
| `api/client.ts` | 新接口与字段 |

---

## 5. 数据模型

### 5.1 `channels` 新增列（仅 `type=weixin_ilink` 使用）

| 列 | 类型 | 说明 |
|----|------|------|
| `ilink_bot_token` | varchar(512) | 扫码所得凭证。API 不回显，只给 `has_bot_token` |
| `ilink_bot_id` | varchar(128) | bot 账号 ID（`…@im.bot`） |
| `ilink_owner_user_id` | varchar(128) | 扫码人微信 ID |
| `ilink_base_url` | varchar(512) | 空 = 默认基址 |
| `ilink_generation` | int, default 0 | 每次扫码成功 +1；用于 runner 重启判定与游标 CAS |
| `ilink_session_expired_at` | datetime null | 非空 = 会话已失效（`-14`），需重扫。扫码成功时清空 |

`default_agent`、`allowed_agents`、`auto_route_*`、`enabled` 复用。`Type` 注释补 `weixin_ilink`（12 字符，`size:16` 足够）。

### 5.2 新表 `channel_ilink_peers`（名册；白名单唯一真相）

| 列 | 类型 | 说明 |
|----|------|------|
| `channel_uuid` | varchar(36) PK | `channels.id` |
| `peer_id` | varchar(128) PK | **原始**微信用户 ID（无 `user:` 前缀） |
| `allowed` | bool, default false | 白名单标志 |
| `remark` | varchar(128) | 备注名（iLink 不提供昵称） |
| `context_token` | varchar(512) | 最近一次入站 token；API 不回显，只给 `has_context_token` |
| `context_token_at` | datetime null | |
| `last_inbound_at` | datetime null | |
| `last_preview` | varchar(160) | 最近发言前 40 字 |
| `created_at` / `updated_at` | datetime | |

白名单放名册而不是 `channels` 列：微信 ID 是不透明串，管理员必须先"看到"对端再放行。

### 5.3 新表 `channel_ilink_state`（长轮询运行时状态）

| 列 | 类型 | 说明 |
|----|------|------|
| `channel_uuid` | varchar(36) PK | `channels.id` |
| `generation` | int | 写入该游标时的 `ilink_generation` |
| `get_updates_buf` | text | 游标 |
| `updated_at` | datetime | |

不放进 `channels`：避免高频变化干扰 Gateway 配置 diff 与 `updated_at` 语义。

### 5.4 `cron_tasks` 新增列

| 列 | 说明 |
|----|------|
| `delivery_peer_id` varchar(128) | 原始微信用户 ID；`delivery_mode=channel` 且渠道为 `weixin_ilink` 时必填 |

### 5.5 Runtime Status 派生

`weixin_ilink` 读路径优先级（高 → 低）：

1. `enabled=false` → `disabled`
2. `ilink_bot_token` 为空 → `login_required`（原因：未登录）
3. `ilink_session_expired_at` 非空 → `login_required`（原因：会话过期，请重新扫码）——**不受心跳新鲜度影响**
4. 心跳缺失或超过 90s → `unknown`
5. 否则取行内 `state`（`connected` / `reconnecting`）

Gateway runner 运行期间**独立于长轮询**每 ≤30s 上报一次心跳（长轮询单轮约 35s，不能兼作心跳）。

### 5.6 Runtime API（Gateway → Portal，service token 鉴权）

路径中的 `{channel_id}` 为 slug（Gateway 侧已知），Portal 内部换算为 `channels.id`。

| 方法 & 路径 | 请求体 | 说明 |
|-------------|--------|------|
| `GET /runtime/v1/gateway/channels` | — | 既有接口；对 `weixin_ilink` 额外返回 `bot_token`、`base_url`、`generation`、`session_expired`、`allowed_peers` |
| `POST /runtime/v1/gateway/channels/{channel_id}/ilink/peers` | `{ generation, peer_id, context_token, preview, at }` | upsert 名册；新对端 `allowed=false`。**`generation` 不匹配 → `409 ILINK_GENERATION_MISMATCH`**，不写入（防止换号后旧 runner 写回旧 bot 的 `context_token`），runner 收到后退出。返回 `{ allowed }`（仅供日志，白名单判定仍以同步副本为准） |
| `GET /runtime/v1/gateway/channels/{channel_id}/ilink/cursor` | — | `{ generation, get_updates_buf }`；`generation` 与当前不符时返回空游标 |
| `PUT /runtime/v1/gateway/channels/{channel_id}/ilink/cursor` | `{ generation, get_updates_buf }` | **CAS**：`generation != channels.ilink_generation` → `409 ILINK_GENERATION_MISMATCH`，runner 丢弃并退出 |
| `POST /runtime/v1/gateway/channels/{channel_id}/ilink/session-expired` | `{ generation, reason }` | `generation` 匹配时写 `ilink_session_expired_at` 并清游标；不匹配忽略（旧 runner 迟到） |
| `POST /runtime/v1/gateway/channels/{channel_id}/status` | 既有 `StatusBody` | 心跳 / `connected` / `reconnecting` |

`allowed_peers` 由名册派生；该接口本就以 service token 下发 `wecom_bot` secret，安全级别一致。白名单改动最长约 15s 生效。

---

## 6. 流程

### 6.1 扫码登录（Portal）

权限：与现有 Channel 管理接口一致（登录用户）。

1. 创建 `weixin_ilink` 渠道 → 无 token，派生 `login_required`。
2. `POST /api/v1/channels/{id}/ilink/login` → Portal 用默认基址调 `get_bot_qrcode` → `AuthEphemeralRepo.Put`（`id = hex(sha256(qrcode))`（64 字符，适配 `auth_ephemeral.id size:64`），`kind = ilink_qrcode`，payload = channel uuid，TTL 5 分钟）→ 返回 `{ qrcode, qrcode_img_content }`。
3. 前端调用 `GET /api/v1/channels/{id}/ilink/login/status?qrcode=…`：
   - Portal 先用 `AuthEphemeralRepo.Get`（**新增的非消费读**：`Get(ctx, id, kind, now)`）校验 `qrcode` 属于该渠道，否则 `404`（防止把别人的确认结果写进任意渠道）。只在 `confirmed` 时调用 `Consume`。
   - Portal 调 `get_qrcode_status`，单次客户端超时 35s（服务端实测挂起约 30s 后返回 `wait`），返回后前端**立即**发起下一次；若服务端立即返回，Portal 不做额外等待，前端最小间隔 1s。
   - `wait` / `scaned`：统一提示"请用微信扫码，并在手机上确认登录"。**不依赖 `scaned`**：实测未出现该状态，直接从 `wait` 跳到 `confirmed`。
   - `expired`：提示并允许"刷新二维码"（回到第 2 步）。
   - `confirmed`：**单事务**：
     - 写 `ilink_bot_token` / `ilink_bot_id` / `ilink_owner_user_id` / `ilink_base_url`；`ilink_generation += 1`；`ilink_session_expired_at = NULL`；
     - 清空 `channel_ilink_state`；
     - **清空该渠道所有名册的 `context_token`**（每次扫码都会换新 bot，旧 token 一律不可用，实测），保留 `allowed` / `remark`（同一微信用户的 ID 跨 bot 不变，已对扫码人实测）；
     - upsert owner 到名册并 `allowed=true`；
     - `AuthEphemeralRepo.Consume` 作废该二维码记录。
4. Gateway 下次同步时 `IlinkConnectionChanged=true`（`Generation` 变化）→ 重启 runner → 旧 runner 迟到的游标写入被 CAS 拒绝。

### 6.2 入站对话（Gateway runner）

```text
启动 → GET 游标（generation 不符 → 空游标）
心跳 goroutine：每 ≤30s 上报 connected / reconnecting
loop:
  getupdates(buf) ──客户端超时──▶ 空批次，继续
       │ -14 ──▶ §7 会话过期（上报后 runner 挂起，不再请求 iLink）
       │ 网络/5xx ──▶ 退避重试（reconnecting）
       ▼
  for msg in msgs where message_type == 1:
     ① 上报名册（generation / context_token / preview / at）——在白名单之前；409 → 退出
  ② PUT 游标（CAS；失败重试 3 次，仍失败记日志后继续——见 §7）
  for msg in msgs where message_type == 1:
     ③ 进程内幂等：ilink:{channel_id}:{message_id} 已见 → 跳过
     ④ 白名单：每条消息从 Registry.Get(channelID) 读取最新 AllowedPeers；
        不在名单 → 回"无权限，请联系管理员开通（ID 尾号 xxxx）"（同一 peer 10 分钟内最多回一次），结束
     ⑤ 非文本 → 回"暂只支持文字消息"，结束
     ⑥ 投递到该 peer 的串行队列（队列上限 5；满则回"消息太多，请稍后再试"）
```

**先提交游标、再处理**：语义为至多一次。崩溃时在途 / 排队中的 Turn 丢失，与 `wecom_bot` 一致；不承诺跨进程去重。

单条消息在 peer 队列中的处理（复用现有流水线，peer 键 `user:{from_user_id}`）：

1. pending `/switch` 序号 → slash 指令 → `prepareAutoRoute`（`@Agent` / 分类器）→ Resolve → **`TurnsStream`**，Gateway 侧 context 超时 600s（同 `wecom_bot` 的 `turn_timeout`）。
2. 消费流事件：15s 内未收到 final → 先 `sendmessage` 一条"处理中…"（每条入站最多一次）；HITL 识别复用 `wecom_progress` 现有逻辑，命中则回"该操作需要在 Web 端确认"。
3. 回复：只取助手正文（不含工具时间线）→ Markdown 简化为纯文本（去标题 / 加粗标记，代码块保留内容）→ 按 `split_runes` 切分（默认 4000 字；实测纯数字 8000 字、中文 4000 字 / 约 11.9 KB 均可完整送达，留余量）→ 依次 `sendmessage`，均带本条消息的 `context_token`。

并发：同 peer 串行、跨 peer 并发；peer 队列空闲 5 分钟回收。

### 6.3 出站推送（Portal）

统一入口在 biz 层：

```go
func (uc *WeixinIlinkUsecase) Push(ctx context.Context, channelUUID, peerID, text string) error
```

1. 查名册：不存在或 `allowed=false` → `目标 {peer} 未在白名单`；`context_token` 为空 → `对方尚未与 bot 对话（或 bot 已重新登录），请对方先给 bot 发一条消息`；渠道 `login_required` → `微信 bot 需重新扫码登录`。均**不调用** iLink。
2. 整形与切分同 §6.2，逐条调用 `internal/ilink.SendText`。**不重试**：`sendmessage` 失败无法判断是否已送达，重试会导致重复消息；Recorder 记录时 `MaxAttempts=1`。
3. 返回 `-14` → 写 `ilink_session_expired_at`（等价于 Runtime `session-expired`，`generation` 取当前值），并返回错误。

**Cron 投递**

- 校验（create / update 时）：`delivery_mode=channel` 且渠道为 `weixin_ilink` → `delivery_peer_id` 必填，且须在该渠道名册中 `allowed=true`。
- `deliverChannel` 新增 `case "weixin_ilink"` → `WeixinIlinkUsecase.Push`；"仅支持 …"错误文案同步更新。
- **Portal 手选**：Cron 表单选中 `weixin_ilink` 渠道后显示对端下拉（`allowed=true`，显示 `remark` 或 ID 尾号）。
- **推回发起人**：在 Portal 的 `portalCronClient.Create` 中，当 `DeliveryMode == ""`：
  1. 从 ctx 取 `tool.ContextKeySessionID`；
  2. `channelPeerSessionRepo.GetBySessionID` 反查 `(channel slug, peer)`；
  3. `channelRepo.GetByChannelID(slug)` 得到渠道；若 `type=weixin_ilink`，回填 `delivery_mode=channel`、`delivery_channel_id=channels.id`、`delivery_peer_id=strip("user:", peer)`；
  4. 否则按现有逻辑默认 `none`。
  
  显式传入的投递参数始终优先。

**`send_to_weixin` 工具**

| 参数 | 必填 | 说明 |
|------|------|------|
| `text` | 是 | 发送内容 |
| `peer_id` | 否 | 省略时：当前会话来自 `weixin_ilink` → 发给当前对端；否则报错要求指定 |
| `channel_id` | 否 | 渠道 slug；仅在下述解析无法唯一确定渠道时需要 |

渠道解析顺序：

1. 当前会话经 `GetBySessionID` 反查到 `weixin_ilink` 渠道 → 用它；
2. 否则 `ListIlinkByAgent(agentID)` 恰好 1 个 → 用它；
3. 否则若传了 `channel_id` 且在 2 的结果中 → 用它；
4. 否则报错（列出可选渠道 slug）。

注册条件：Agent 至少关联一个启用的 `weixin_ilink` 渠道（`ListIlinkByAgent` 非空）。检索词 `微信`、`weixin`、`推送`；复用 `DeliveryRecorder`（`MaxAttempts=1`）与按会话频率限制（同 `send_to_wecom`）。

### 6.4 名册管理（Portal）

| 接口 | 说明 |
|------|------|
| `GET /api/v1/channels/{id}/ilink/peers` | 列表：`peer_id`、`allowed`、`remark`、`has_context_token`、`last_inbound_at`、`last_preview` |
| `PATCH /api/v1/channels/{id}/ilink/peers/{peer_id}` | 改 `allowed` / `remark` |
| `DELETE /api/v1/channels/{id}/ilink/peers/{peer_id}` | 移出名册（下次发言会重新入册且默认拒绝） |

权限同 §6.1。

---

## 7. 错误处理

| 场景 | 处理 |
|------|------|
| `getupdates` 网络错误 / 5xx | 指数退避 1s → 60s 封顶；心跳上报 `reconnecting`；恢复后 `connected` |
| `-14`（入站） | 立即停止请求 iLink；调 `session-expired`；runner **挂起但保留在 manager 的 active 表**，避免 `Reconcile` 每 15s 重启；下次同步看到 `session_expired=true` 后 stop（`reportDisabled=false`，不写误导性的 `disabled`）；重扫后 `Generation` 变化再启动 |
| 名册上报 `409` | 当前 runner 已过期（重扫 / 换号），丢弃本批并退出 |
| 名册上报失败 | 记日志，不阻塞（`context_token` 可能缺失 → 该 peer 暂无法被主动推送，下次发言自愈） |
| 游标 PUT 失败 | 重试 3 次仍失败：记日志并继续处理本批；若随后进程重启会重复拉取这批消息，可能重复回复（已知限制，仅在 Portal 不可用 + 进程重启叠加时发生） |
| 游标 PUT `409` | 当前 runner 已过期（重扫过），丢弃本批并退出 |
| Turn 超时 / 出错 | 回"处理失败：{简述}"，不静默丢弃 |
| 回复需要 HITL | fail-closed："该操作需要在 Web 端确认" |
| 出站 `-14` | 写 `ilink_session_expired_at`；返回错误（Cron 记入 `delivery_error`） |
| 出站无 token / 不在白名单 / 需重扫 | 明确报错，不调 iLink |
| 日志脱敏 | `bot_token`、`context_token` 一律不打日志；API 只给 `has_*` |

---

## 8. 测试

**协议层（Gateway、Portal 各一份）**

- `httptest` 模拟 iLink：请求头齐全且 `X-WECHAT-UIN` 为合法 base64；`getupdates` 游标推进；客户端超时 → 空批次；`-14`（`ret` / `errcode` 两种）；`sendmessage` body。
- 两模块各持一份 `testdata/ilink/*.json`（内容相同，人工同步）。

**Gateway**

- Manager：启用 / 停用、`Generation` 变化重启、无 token 或 `SessionExpired` 不启动、改 `AllowedPeers` **不**重启、`-14` 后挂起不被 `Reconcile` 反复拉起。
- 入站：名册上报先于白名单；游标先于处理提交；白名单每条读 Registry（同步更新后立即生效）；"无权限"10 分钟限频；幂等去重；非文本提示；slash 与 `/switch` 两步；"处理中"（<15s 不发、>15s 发一次）；HITL fail-closed；长文本切分；同 peer 串行 / 跨 peer 并发；队列满提示。
- 游标 / 名册上报 `409` → runner 退出；网络错误退避；心跳间隔 ≤30s。
- `channelsync` 组合多个 reconciler。

**Portal**

- 扫码：`qrcode` 与渠道绑定校验；`wait` / `scaned` / `confirmed` / `expired`；`confirmed` 单事务（token、generation+1、清失效标记、清游标、owner 放行、清空全部名册 token 但保留 `allowed` / `remark`）。
- Runtime API：名册 upsert（含 `generation` 不匹配 `409` 不写入）、游标 CAS（匹配 / 不匹配）、`session-expired` 迟到忽略、`gateway/channels` 下发字段与类型过滤。
- `AuthEphemeralRepo.Get` 非消费读：多次轮询均可校验；`confirmed` 后 `Consume`，再轮询 `404`。
- `DeriveRuntimeStatus` 五级优先级（含"失效 + 心跳新鲜"仍为 `login_required`）。
- `channelRepo.Update` 无法写入 `ilink_bot_token`；删除渠道级联清理。
- `Push`：缺 peer / 不在白名单 / 无 token / 需重扫 / 成功 / `-14` 写失效；不重试。
- Cron：create/update 校验；`portalCronClient.Create` 回填（有反查 / 非 iLink 会话 / 显式参数优先 / framework 传空 mode）。
- `send_to_weixin`：渠道解析四步、`peer_id` 默认值、注册条件。

**端到端验收（写入 Gateway README）**

1. 创建渠道 → 扫码 → ≤15s `connected`；owner 已在名册且放行。
2. 非白名单微信号发消息 → "无权限"；名册出现该对端与预览。
3. 名册放行 → ≤15s 后对话正常；`/who`、`/switch` 可用。
4. 微信内让 Agent 建 Cron → 到点推回本人。
5. Portal 手建 Cron 选对端 → 推送成功；选未对话过的对端 → 保存即被拒或 `delivery_error` 明确。
6. Web 会话中 `send_to_weixin` 指定 `peer_id`（必要时 `channel_id`）推送成功。
7. 会话过期 → 持续显示 `login_required`（不会退化为 `unknown`）→ 重扫后恢复。
8. 停 Gateway → ≤90s `unknown`；Cron 推送仍可用。

---

## 9. 风险

| 项 | 说明 / 缓解 |
|----|-------------|
| 协议非正式开放 | 以官方 OpenClaw 插件形态交付，HTTP 协议由源码公开，可能变动。协议常量集中在两侧 `internal/ilink` |
| `context_token` 有效期未知 | 200 响应无法判断是否送达；记录 `context_token_at` 供排查，后续视实测补"过旧 token 告警" |
| 重扫后推送中断 | 每次扫码换新 bot，名册 token 全部清空；在对方重新发言前，Cron / `send_to_weixin` 一律失败。扫码成功页提示"请让需要接收推送的人先给 bot 发一条消息"；名册显示"待激活"（无 token） |
| 单副本约束 | 同一 bot 多实例同时长轮询会重复收消息；与 `wecom_bot` 相同，文档明确 |
| 至多一次 | 崩溃丢在途 Turn；Portal 不可用 + 重启叠加时可能重复回复 |

---

## 10. 实现前验证（Phase 0 spike）

用真实微信号与官方协议做一次性验证，结论回写本规格；若结论推翻假设，再调整设计：

工具：`scripts/ilink-spike/`（计划见 `docs/superpowers/plans/2026-10-18-weixin-ilink-p0-spike.md`）。实测日期 2026-10-18，单个微信号（ID 尾号 `gkx4`）。原始日志只保存在本地 `scripts/ilink-spike/.state/`，不入库。

1. **非扫码人能否私聊该 bot？** 若只有扫码人能发消息，名册审批 UI 可大幅简化（只剩 owner）。
   - **结论：未验证**（缺第二个微信号）。设计保持名册 + 白名单（两种情况都适用）；有条件时补测，若确认只有扫码人可对话，名册审批 UI 可移出一期。
   - 已确认：扫码返回的 `ilink_user_id` 与该用户消息的 `from_user_id` 完全一致，§6.1"owner 入名册并放行"成立。微信里始终只有一个 bot 会话（换 bot 对用户不可见）。
2. **`get_qrcode_status` 是否服务端挂起？** 决定 §6.1 第 3 步的等待参数。
   - **结论：是**。无事件时服务端挂起约 30.1s 后返回 `wait`（连续多次一致）；手机确认后，挂起中的请求在第 24.5s / 23.9s 即返回 `confirmed`，事件能立即唤醒。**未出现 `scaned`**。§6.1 改为 35s 客户端超时、不依赖 `scaned`。
3. **同一 bot 重扫后旧 `context_token` 是否仍有效？** 决定 §6.1 是否也要清空同号 token。
   - **结论：不存在"同一 bot 重扫"**——同一微信号重扫得到**新的 `ilink_bot_id`**；旧 `bot_token` 立即返回 `-14`；旧 `context_token` 配新 `bot_token` 发送返回 200 但**未送达**（对照组：重扫前同一 token 正常送达）。§6.1 改为每次扫码都清空名册 token；§9 补"重扫后推送中断"。
4. **换 bot 后同一微信用户的 `from_user_id` 是否不变？** 决定换号时是否保留 `allowed` / `remark`。
   - **结论：不变**（扫码人实测：两次登录、两个 bot，`ilink_user_id` 相同）。保留 `allowed` / `remark`。非扫码人未验证，随问题 1 一并补测。
5. **单条文本长度上限**，校准切分值。
   - **结论：未触及上限**。纯数字 1000 / 2000 / 4000 / 8000 字，中文 1000 / 2000 / 4000 字（最大 11906 字节）均完整送达、未拆分、未折叠。切分默认值改为 4000 字并做成配置项 `split_runes`。

额外观察：
- `sendmessage` 成功响应为 `{"message_id": …}`，不是空对象。
- bot 自己发出的消息不出现在 `getupdates`。
- 未观察到群聊（`group_id` 均为空）。
- `getupdates` 响应同时带已废弃的 `sync_buf`，忽略即可；消息还有 `seq`、`session_id`（空）、`update_time_ms` 等字段，一期不使用。
- 同一 `context_token` 5 分钟内连续发 9 条均送达，未见回复条数限制。
