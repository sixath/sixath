# Sixath Inbound Gateway

统一入站对话入口：浏览器 **Web**、通用 **Webhook**，以及企业微信 **智能机器人 AI+** 的 **WebSocket 长连接**（`type=wecom_bot`）。

Gateway **不跑** ReAct / 不持有工具真相；协议适配与 `peer→session` 路由在此完成，Agent 执行走 Portal `/runtime/v1`。

| 文档 | 说明 |
|------|------|
| 本 README | 架构 + 本地用法 + 企微长连接配置 |
| [入站 Gateway 设计](../docs/superpowers/specs/2026-08-09-inbound-gateway-design.md) | Web / Webhook 契约 |
| [Portal 入站 Agent 路由](../docs/superpowers/specs/2026-08-10-gateway-portal-agent-routing-design.md) | default/白名单、改绑、指令 |
| [企微 `/switch` 两步绑定](../docs/superpowers/specs/2026-08-11-wecom-switch-agent-design.md) | 序号选择 Agent、pending TTL |
| [企微智能机器人设计](../docs/superpowers/specs/2026-08-09-wecom-bot-gateway-design.md) | 长连接 Adapter |
| [渠道 Portal 真相源 + Runtime Status](../docs/superpowers/specs/2026-08-11-channel-portal-runtime-status-design.md) | 配置同步、连接态 |
| 官方 | [智能机器人长连接](https://developer.work.weixin.qq.com/document/path/101463) |

进程配置：`configs/config.example.yaml`。

**渠道配置权威在 Portal**（Web Channels UI / Admin API）。Gateway 定时从 `GET /runtime/v1/gateway/channels` 拉取并热更新；**运行时不再读取** `configs/channels.yaml`。该文件仅作归档 / 一次性 import，见下文 [渠道配置（Portal）](#渠道配置portal)。

**Agent 路由权威在 Portal**，见下文 [Agent 路由与改绑](#agent-路由与改绑)。

---

## 架构

```text
┌─────────────┐     SSE / REST        ┌──────────────────────────────┐
│  Web UI     │──────────────────────▶│          Gateway             │
└─────────────┘                       │  · ChannelRegistry           │
                                      │  · SessionRouter (peer缓存)   │
┌─────────────┐   POST /hooks/{id}    │  · Idempotency (msgid)       │
│  Webhook    │──────────────────────▶│  · RuntimeClient             │
│  调用方     │◀── 202 / sync JSON    │  · ChannelSync (~15s)        │
└─────────────┘                       │                              │
                                      │  Adapters:                   │
                                      │   web · webhook · wecom_bot  │
┌─────────────┐   WSS (Gateway连出)   │                              │
│ 企微 openws │◀─────────────────────▶│  wecom: subscribe / ping /   │
│ 智能机器人  │   aibot_msg_callback  │  respond_msg(stream)         │
└─────────────┘   aibot_respond_msg   └──────────────┬───────────────┘
                                                     │ Bearer runtime_token
                                                     │ X-Sath-User-Id
                                                     ▼
                                      ┌──────────────────────────────┐
                                      │  Portal /runtime/v1          │
                                      │  resolve · turns · channels  │
                                      │  Agent / tools / memory      │
                                      └──────────────────────────────┘

Portal Channel type=wecom (群 Webhook URL)  ──▶  send_to_wecom / Cron（只出站，不经 Gateway）
```

### 职责边界

| 组件 | 做什么 | 不做什么 |
|------|--------|----------|
| **Gateway** | 从 Portal 同步渠道、协议适配、入站鉴权、`channel+peer→session` 缓存、slash 指令翻译、回复形态（SSE / ack / 企微 stream 卡片）、`wecom_bot` 连接态上报 | 不跑 Agent、不持有渠道/白名单真相、不注册业务工具 |
| **Portal Runtime** | 渠道 CRUD 真相源、会话持久化、Turn 执行、工具/记忆/HITL、Runtime Status 派生 | 不直接对接企微长连接入站 |
| **Portal `type=wecom`** | Agent 工具 / Cron **单向推群** | 不负责 @续聊入站 |

### 渠道一览

| `type` / 入口 | 入站 | 出站回复 | 会话键 |
|---------------|------|----------|--------|
| Web（经 Gateway） | 登录用户 Bearer | SSE 流式（含 tool/model 时间线） | 显式多 `session_id`（不按 peer 折叠） |
| `webhook` | `POST /hooks/{channel_id}` | 默认异步 202 + `reply_url`；可同步 | `channel_id + peer_id` |
| `wecom_bot` | 企微 WSS 推送 `aibot_msg_callback` | `aibot_respond_msg` stream 卡片 | 群 `chat:{chatid}` / 单聊 `user:{userid}` |

### 企微消息路径（`wecom_bot`）

```text
aibot_msg_callback
    → Normalize（去 @bot、peer、问题正文）
    → 可选 Directory：userid → 通讯录显示名（corp_id + corp_secret）
    → Idempotency(msgid)
    → pending /switch 或 slash：直接 finish=true 卡片（不发「处理中…」）
    → 业务 Turn：respond finish=false「处理中…」
    → Runtime resolve + turns(reply_mode=stream)
    → respond finish=true 卡片（发起人 / 问题 / 答复）
```

- **final** 出站只含助手正文，**不含** Web 调试时间线 / `debugRun` 工具过程。
- HITL（确认卡等）在企微面 fail-closed，提示去 Web。
- 同一 `bot_id` **同时只能一条**有效长连接（多 Gateway 实例勿重复 `enabled`）。

---

## 渠道配置（Portal）

**配置源 = Portal。** 在 Web **Channels** 创建/编辑 `webhook` / `wecom_bot`（协议密钥、enabled、default_agent、allowed_agents）。Gateway 约每 15s 全量拉取并 diff 热更新；改配置后通常无需重启 Gateway。

进程 yaml 里的 `channels_file` **已废弃、运行时忽略**。仓库内 `configs/channels.yaml` 仅作归档与一次性导入，**不是**运行时真相源；Compose **不再挂载**该文件。

### 从旧 yaml 导入

若环境里仍有历史 `channels.yaml`，一次性 upsert 到 Portal：

```bash
cd portal
go run ./cmd/import-gateway-channels -config ./configs -channels ../gateway/configs/channels.yaml
# 预览：
go run ./cmd/import-gateway-channels -config ./configs -channels ../gateway/configs/channels.yaml -dry-run
```

导入后请在 Portal UI 核对凭证与 Agent 绑定；勿再依赖改 yaml + 重启 Gateway。

### Runtime Status（仅 `wecom_bot`）

Web Channels 列表 / 编辑页展示连接态。Gateway 上报 `connected` / `disconnected` / `reconnecting` / `disabled`；Portal 读路径再派生 **`unknown`**。

| 状态 | 含义 |
|------|------|
| `connected` | 长连接正常 |
| `disconnected` | 已断开 |
| `reconnecting` | 重连中 |
| `disabled` | 渠道 `enabled=false`（或 runner 已停） |
| `unknown` | 无心跳行，或心跳超过 **90s** 未刷新（如 Gateway 停机） |

其它类型（`webhook` 等）列表显示 `—`。

### 单副本约束

同一 `bot_id` **不要**在多台 Gateway（含 K8s 多副本）上同时 `enabled`。新的 `aibot_subscribe` 会踢掉旧连接；重复订阅时日志常见 `wecom_bot <id> disconnected` 重连循环。

---

## Agent 路由与改绑

入站 **`channel_id` 必须在 Portal `channels` 表中有对应行**。Gateway Resolve 只认 Portal 上的 `default_agent` 与 `allowed_agents`；Portal 无该渠道时 **fail-closed**（`CHANNEL_NOT_FOUND`）。改 default / 白名单走 Portal 管理 API 或 UI，**下一请求即生效**，无需重启 Gateway。

| 配置项 | 权威 | 说明 |
|--------|------|------|
| `bot_id`、`secret`、webhook_secret、IP 白名单等 | **Portal `channels`** | Gateway 定时同步热更新 |
| `default_agent`、`allowed_agents` | **Portal `channels`** | 即时生效；Gateway 经 Resolve / agents API 使用 |
| `channel+peer → session` | Portal `channel_peer_sessions` | 改绑时 upsert；Gateway SessionRouter 为短缓存 |

旧 yaml 里的 **`default_agent` 可选、已废弃为路由真相**。新环境请在 Portal 为每个入站 `channel_id` 配置 default 与白名单。

### Slash 指令（Webhook / 企微等真人渠道）

在业务 turn 之前拦截；成功时返回短确认，**本条不跑 Agent turn**。

| 指令 | 行为 |
|------|------|
| `/switch` | 列出白名单（标注**当前**绑定）；**2 分钟内**回复纯数字序号完成 `force_new` 改绑；本条不 Turn |
| `/who` | 只读查看当前 `channel+peer` 绑定（名/id + 短 session）；不 Put pending、本条不 Turn |
| `/agent <id\|name>` | `force_new=true`，切到白名单内 Agent 并新开 session |
| `/agent` 或 `/agents` | 列出本渠道 default + allowed（Portal） |
| `/new` | `force_new=true`，沿用当前映射 Agent 或 default，新开 session |
| `/unbind` | 清除 `channel+peer` 映射；下一条普通消息按 default 新建 |

**`/switch` 两步绑定（企微优先）：** 发 `/switch` 后，下一条仅接受 `1`/`2`/…（`/1` 不算序号）。非法输入立刻 `finish=true` 提示「没有发给 Agent」，pending 保留；超时后 pending 取消，消息按普通入站处理。处理顺序：**pending 序号 → slash → Resolve/Turn**。只想看当前绑了谁请用 `/who`（不进入选号窗口；窗口内发 `/who` 也不取消选号）。设计见 [企微 /switch 两步绑定](../docs/superpowers/specs/2026-08-11-wecom-switch-agent-design.md)。

Webhook body 可选 `agent_id` / `force_new`（与指令等价；白名单仍由 Portal 校验）。详见 [Agent 路由设计](../docs/superpowers/specs/2026-08-10-gateway-portal-agent-routing-design.md)。

---

## 本地运行

```bash
cd gateway
go run ./cmd/gateway -config ./configs/config.example.yaml
# 或
go build -o ./bin/gateway.exe ./cmd/gateway
./bin/gateway.exe -config ./configs/config.example.yaml
```

默认：

| 项 | 值 |
|----|-----|
| 监听 | `:8088` |
| Portal | `http://127.0.0.1:8000` |
| Runtime token | `dev-runtime-token`（须与 Portal `runtime.service_token` 一致） |
| 渠道配置 | Portal（`GET /runtime/v1/gateway/channels`） |
| Turn 超时 | 120s |

依赖：Portal 已启动且 `/runtime/v1` 可用；渠道须已在 Portal 配置（或先跑 import）；Web 对话还需 Vite / 鉴权对齐（见仓库根 README）。

---

## 企微智能机器人（长连接 `wecom_bot`）

长连接模式下 Gateway **主动连出** 企微 WSS，**不需要**公网 HTTPS 回调 URL，也**不要**配置 URL 模式用的 `callback_token` / `EncodingAESKey`。

### 1. 控制台

企业微信管理后台 → **智能机器人 AI+** → **API 配置**：

1. 接入方式选 **「使用长连接」**（与「接收消息 URL 回调」互斥）。
2. 复制 **BotID**，获取 **Secret**（长连接专用，通常只展示一次）。
3. 在 **Portal Web → Channels** 创建 `type=wecom_bot` 渠道并填写凭证，**不要**把真实密钥提交到 Git。

### 2. Portal `wecom_bot` 字段

| 字段 | 说明 |
|------|------|
| `channel_id` | 入站 **`channel_id`**（与 Resolve / hooks 路径一致）。 |
| `type` | 固定 `wecom_bot`。 |
| `enabled` | `true` 时 Gateway 启动 WS 并 `aibot_subscribe`。 |
| `default_agent` / `allowed_agents` | Agent 绑定，Portal 权威。 |
| `bot_id` / `secret` | 控制台 BotID + 长连接 Secret（`enabled: true` 时必填）。 |
| `bot_names` | 可选。群内显示名，用于剥离 `@提及`。 |
| `ws_url` | 可选。默认 `wss://openws.work.weixin.qq.com`。 |
| `corp_id` / `corp_secret` | 可选。自建应用凭证（需成员读取）；用于把「发起人」从加密 userid 解析成显示名。 |

显示名解析链：`gettoken` →（必要时）`openuserid_to_userid` → `user/get`；失败不阻塞回复。

### 3. 与 Portal 群 Webhook 分离

| 能力 | 承载 | 凭证 |
|------|------|------|
| 群/单聊 @ 续聊、对话回复 | Gateway `wecom_bot` | `bot_id` + `secret` |
| 工具/Cron **单向推群** | Portal `type=wecom` Webhook | Webhook URL（Portal 渠道配置） |

两套凭证**不可混用**。`send_to_wecom` 仍走 Portal，无需改成长连接。

### 4. 验收清单

1. 启动后连接稳定，无 `wecom subscribe rejected`，无立刻重连风暴；UI Runtime Status → `connected`（约 ≤15s）。
2. **单聊**文本 → stream 卡片含 **发起人**、**问题**、Agent 答复（无工具过程日志）。
3. **群 @** → 同上；会话按 `chat:{chat_id}` 续聊；「问题」为去掉 @ 后的正文。
4. 配置了 `corp_id`/`corp_secret` 时，「发起人」为通讯录显示名；否则可为加密 userid。
5. Portal `send_to_wecom` 推群仍可用。
6. 停 Gateway → 约 ≤90s UI 显示 `unknown`。

回复卡片：

```text
发起人：{asker_name}
问题：{question_text}

{assistant_answer}
```

失败时仍 `finish=true` 出站失败卡片（含发起人/问题），不静默丢弃。

---

## 其它渠道

### Webhook

```http
POST /hooks/{channel_id}
```

见仓库 E2E：`_neo4j_q/verify_inbound_gateway.ps1`（默认探测 `:8088` / `:8000` 或 Compose 映射端口）。渠道须已存在于 Portal（可用 import 写入 demo-webhook）。

### Web

经 Portal 鉴权的浏览器会话；Gateway 代理 SSE。与 `wecom_bot` 并行，共享 Session / 幂等存储。Web 时间线（模型推理、工具调用）仅 SSE 展示，不写入企微卡片。

---

## 开发提示

```bash
cd gateway
go test ./internal/wecom/ ./internal/channel/ ./internal/adapter/ ./internal/channelsync/ -count=1
```

改 Portal 渠道配置后 Gateway 会热同步；改 Gateway 二进制后需**重启**进程。
