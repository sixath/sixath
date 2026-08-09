# Sixath Inbound Gateway

统一入站对话入口：浏览器 **Web**、通用 **Webhook**，以及企业微信 **智能机器人 AI+** 的 **WebSocket 长连接**（`type=wecom_bot`）。

Gateway **不跑** ReAct / 不持有工具真相；协议适配与 `peer→session` 路由在此完成，Agent 执行走 Portal `/runtime/v1`。

| 文档 | 说明 |
|------|------|
| 本 README | 架构 + 本地用法 + 企微长连接配置 |
| [入站 Gateway 设计](../docs/superpowers/specs/2026-08-09-inbound-gateway-design.md) | Web / Webhook 契约 |
| [企微智能机器人设计](../docs/superpowers/specs/2026-08-09-wecom-bot-gateway-design.md) | 长连接 Adapter |
| 官方 | [智能机器人长连接](https://developer.work.weixin.qq.com/document/path/101463) |

进程配置：`configs/config.example.yaml`。渠道列表：`configs/channels.yaml`（**勿提交**真实 `bot_id` / `secret` / `corp_secret`）。

---

## 架构

```text
┌─────────────┐     SSE / REST        ┌──────────────────────────────┐
│  Web UI     │──────────────────────▶│          Gateway             │
└─────────────┘                       │  · ChannelRegistry           │
                                      │  · SessionRouter (peer缓存)   │
┌─────────────┐   POST /hooks/{id}    │  · Idempotency (msgid)       │
│  Webhook    │──────────────────────▶│  · RuntimeClient             │
│  调用方     │◀── 202 / sync JSON    │                              │
└─────────────┘                       │  Adapters:                   │
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
                                      │  resolve · turns(stream|final)│
                                      │  Agent / tools / memory      │
                                      └──────────────────────────────┘

Portal Channel type=wecom (群 Webhook URL)  ──▶  send_to_wecom / Cron（只出站，不经 Gateway）
```

### 职责边界

| 组件 | 做什么 | 不做什么 |
|------|--------|----------|
| **Gateway** | 渠道配置、协议适配、入站鉴权、`channel+peer→session`、回复形态（SSE / ack / 企微 stream 卡片） | 不跑 Agent、不注册业务工具 |
| **Portal Runtime** | 会话持久化、Turn 执行、工具/记忆/HITL | 不直接对接企微长连接入站 |
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
    → respond finish=false「处理中…」
    → Runtime resolve + turns(reply_mode=final)
    → respond finish=true 卡片（发起人 / 问题 / 答复）
```

- **final** 出站只含助手正文，**不含** Web 调试时间线 / `debugRun` 工具过程。
- HITL（确认卡等）在企微面 fail-closed，提示去 Web。
- 同一 `bot_id` **同时只能一条**有效长连接（多副本勿重复 `enabled`）。

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
| 渠道文件 | `./configs/channels.yaml` |
| Turn 超时 | 120s |

依赖：Portal 已启动且 `/runtime/v1` 可用；Web 对话还需 Vite / 鉴权对齐（见仓库根 README）。

---

## 企微智能机器人（长连接 `wecom_bot`）

长连接模式下 Gateway **主动连出** 企微 WSS，**不需要**公网 HTTPS 回调 URL，也**不要**配置 URL 模式用的 `callback_token` / `EncodingAESKey`。

### 1. 控制台

企业微信管理后台 → **智能机器人 AI+** → **API 配置**：

1. 接入方式选 **「使用长连接」**（与「接收消息 URL 回调」互斥）。
2. 复制 **BotID**，获取 **Secret**（长连接专用，通常只展示一次）。
3. 写入本机 `channels.yaml`，**不要**提交到 Git。

### 2. `channels.yaml` 字段

| 字段 | 说明 |
|------|------|
| `id` | Gateway 内渠道唯一标识（换 Agent 时可 bump，避免旧 peer 映射绑死旧 agent）。 |
| `type` | 固定 `wecom_bot`。 |
| `enabled` | `true` 时启动 WS 并 `aibot_subscribe`。 |
| `default_agent` | 会话绑定的 Agent UUID。 |
| `bot_id` / `secret` | 控制台 BotID + 长连接 Secret（`enabled: true` 时必填）。 |
| `bot_names` | 可选。群内显示名，用于剥离 `@提及`。 |
| `ws_url` | 可选。默认 `wss://openws.work.weixin.qq.com`。 |
| `corp_id` / `corp_secret` | 可选。自建应用凭证（需成员读取）；用于把「发起人」从加密 userid 解析成 `别名(姓名)`。未配置则回退 userid。 |

```yaml
channels:
  - id: xiaotiancai
    type: wecom_bot
    enabled: true
    default_agent: "<agent-uuid>"
    bot_id: "<控制台 BotID>"
    secret: "<长连接 Secret>"
    bot_names: ["小天才"]
    ws_url: "wss://openws.work.weixin.qq.com"
    corp_id: "<企业 CorpID>"          # 可选
    corp_secret: "<自建应用 Secret>"  # 可选，勿提交
```

显示名解析链：`gettoken` →（必要时）`openuserid_to_userid` → `user/get`；失败不阻塞回复。

### 3. 与 Portal 群 Webhook 分离

| 能力 | 承载 | 凭证 |
|------|------|------|
| 群/单聊 @ 续聊、对话回复 | Gateway `wecom_bot` | `bot_id` + `secret` |
| 工具/Cron **单向推群** | Portal `type=wecom` Webhook | Webhook URL（Portal 渠道配置） |

两套凭证**不可混用**。`send_to_wecom` 仍走 Portal，无需改成长连接。

### 4. 单副本约束

同一 BotID 同时只允许一条有效长连接；新的 `aibot_subscribe` 会踢掉旧连接。

- 同一 `bot_id` 不要在多台 Gateway（含 K8s 多副本）上同时 `enabled: true`。
- 重复订阅时日志常见 `wecom_bot <id> disconnected` 重连循环。

### 5. 验收清单

1. 启动后连接稳定，无 `wecom subscribe rejected`，无立刻重连风暴。
2. **单聊**文本 → stream 卡片含 **发起人**、**问题**、Agent 答复（无工具过程日志）。
3. **群 @** → 同上；会话按 `chat:{chat_id}` 续聊；「问题」为去掉 @ 后的正文。
4. 配置了 `corp_id`/`corp_secret` 时，「发起人」为通讯录显示名；否则可为加密 userid。
5. Portal `send_to_wecom` 推群仍可用。

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

见仓库 E2E：`_neo4j_q/verify_inbound_gateway.ps1`（默认探测 `:8088` / `:8000` 或 Compose 映射端口）。

### Web

经 Portal 鉴权的浏览器会话；Gateway 代理 SSE。与 `wecom_bot` 并行，共享 Session / 幂等存储。Web 时间线（模型推理、工具调用）仅 SSE 展示，不写入企微卡片。

---

## 开发提示

```bash
cd gateway
go test ./internal/wecom/ ./internal/channel/ ./internal/adapter/ -count=1
```

改 `channels.yaml` 或二进制后需**重启** Gateway 进程才会加载新配置 / 新代码。
