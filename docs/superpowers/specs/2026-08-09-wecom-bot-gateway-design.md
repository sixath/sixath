# 企微智能机器人 Gateway 双向接入

**日期**: 2026-08-09  
**状态**: 设计已确认；待实现（**长连接**）  
**目标**: 在已落地的入站 Gateway 上增加 **企业微信「智能机器人」** Adapter，支持群/单聊入站对话与应用侧出站回复；Portal 现有 **群 Webhook 出站**（`type=wecom` / `send_to_wecom`）保持不动。

**关联**:
- [入站 Gateway 设计](./2026-08-09-inbound-gateway-design.md)
- [企微群机器人出站（Portal）](../../../portal/docs/superpowers/specs/2026-06-04-wecom-bot-design.md)
- 官方：[智能机器人长连接](https://developer.work.weixin.qq.com/document/path/101463)

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 企微产品 | **智能机器人 AI+**（非自建应用） |
| 连接方式 | **长连接（WebSocket）** — Gateway **无公网回调** |
| 架构模式 | **方案 2**：Gateway 管智能机器人双向；Portal 保留群 Webhook 只出站 |
| 入站 | 企微 → WSS → Gateway → Runtime `resolve` + `turns(final)` |
| 出站（对话回复） | 长连接 `aibot_respond_msg`（stream markdown） |
| 出站（告警/工具推群） | **仍用** Portal `type=wecom` Webhook + `send_to_wecom` |
| 会话 peer | 群：`chat:{chat_id}`；单聊：`user:{user_id}` |
| 回复卡片 | 必须回显 **发起人** + **问题正文**（去 @机器人） |
| 部署约束 | **同一 BotID 同时只能一条有效长连接**；多 Gateway 副本勿对同一 bot 重复 `enabled` |
| 非目标（本期） | URL 回调模式；自建应用 XML；迁 Portal Webhook；HITL 企微按钮；知识集/工作流产品化 |

**为何改长连接：** 部署侧无法稳定暴露公网 HTTPS 回调；控制台选「使用长连接」，凭 BotID + Secret 由 Gateway **主动连出**即可。

---

## 1. 背景

截图场景（「小天才 BOT」）：

- 在群成员中以 **BOT** 出现，可主动发长文/结构化卡片（出站）
- 支持「引用并 @机器人 可继续」（入站续聊）
- 管理入口为企微 **智能机器人 AI+** → API 配置 → **使用长连接**

与一期 Gateway（`web` + 通用 `webhook`）的关系：智能机器人是 **第二个正式 IM Adapter**，复用 Registry / SessionRouter / RuntimeClient / 幂等；入站不再走 HTTP hook，而是进程内 **WS 客户端**。

与 Portal 群 Webhook 的关系：

| 能力 | 承载 |
|------|------|
| @续聊、对话闭环 | Gateway `wecom_bot`（长连接） |
| Agent 工具单向推群、Cron 投递 | Portal `wecom` Webhook（不动） |

禁止把同一套凭证混用在两种 Channel 类型上。长连接 Secret ≠ URL 回调的 Token/EncodingAESKey。

---

## 2. 架构

```text
企微 openws.work.weixin.qq.com (WSS)
        │  Gateway 主动连出
        ▼
┌───────────────────────────────────────┐
│ Gateway                               │
│  wecom_bot WS client (per channel)    │
│  · aibot_subscribe (bot_id+secret)    │
│  · ping 心跳 ~30s                     │
│  · aibot_msg_callback → Normalize     │
│  · peer → Runtime resolve             │
│  · turns(final)                       │
│  · aibot_respond_msg (stream)         │
└───────────────────────────────────────┘
        │ service token
        ▼
┌───────────────────────────────────────┐
│ Portal /runtime/v1                    │
└───────────────────────────────────────┘

Portal Channel type=wecom (Webhook URL)
        │
        ▼
  send_to_wecom / Cron  ──▶ 企微群（只出站）
```

---

## 3. Gateway 渠道配置

`ChannelRegistry` 新增 `type: wecom_bot`：

```yaml
channels:
  - id: xiaotiancai
    type: wecom_bot
    enabled: true
    default_agent: "<agent-uuid>"
    bot_id: "..."           # 控制台 BotID
    secret: "..."           # 长连接专用 Secret（只显示一次）
    bot_names: ["小天才"]   # 可选；用于剥离 @提及
    ws_url: ""              # 可选；默认 wss://openws.work.weixin.qq.com
```

说明：

- 控制台必须选 **「使用长连接」**（与 URL 回调互斥；切模式会踢掉旧连接/旧回调）
- 凭证只存在 Gateway 配置
- **不要**配置 `callback_token` / `callback_aes_key`（那是 URL 模式）

---

## 4. 入站流程

### 4.1 连接生命周期

1. Gateway 启动（或 channel `enabled`）时：Dial `ws_url` → 发送 `aibot_subscribe`（`bot_id` + `secret`）  
2. 订阅成功后循环读帧；每 ~30s 发 `ping`  
3. 断线：指数退避重连（新连接会踢掉旧连接——符合官方「一 bot 一连接」）  
4. 优雅退出：关闭 WS

### 4.2 消息回调

收到 `cmd=aibot_msg_callback`：

1. 一期只处理 `body.msgtype=text`；其它类型忽略（可打日志）  
2. `headers.req_id` **必须**在后续 `aibot_respond_msg` 中透传  
3. Normalize → 幂等 `body.msgid` → Resolve → 后台 `turns(final)`（不阻塞读循环）

长连接模式下 **没有** URL 模式那种流式刷新回调；由我方主动推 stream 更新。

### 4.3 Normalize（关键）

| 字段 | 来源 |
|------|------|
| `asker_id` | `body.from.userid` |
| `asker_name` | 一期 = userid |
| `question_text` | `body.text.content` 去 `@机器人` |
| `chat_id` | `body.chatid`（群） |
| `msg_id` | `body.msgid` |
| `quoted` | `body.quote`（若有），可附 Runtime content，**不替代** question_text |

注入 Runtime：

```text
[企微] 发起人={asker_name}({asker_id})
问题：{question_text}
```

### 4.4 会话

- 群：`peer_id = chat:{chat_id}`  
- 单聊：`peer_id = user:{asker_id}`  
- `agent_id` = channel `default_agent`（已有映射不换 agent）  
- 同 `msgid` 不重复开 turn

### 4.5 HITL

无企微确认交互面：危险确认 **fail-closed**；出站文案提示去 Web。

---

## 5. 出站（对话回复）

### 5.1 发送

对同一次回调（同一 `req_id`）：

1. **先** `aibot_respond_msg`：`msgtype=stream`，`finish=false`，content 可为「处理中…」（生成稳定 `stream.id`）  
2. turns 完成后 **再** 同 `stream.id`、`finish=true`，content = 回复卡片（markdown）  
3. turns 失败：`finish=true` + 失败卡片（含发起人/问题）；**禁止静默**  
4. 超长：按官方 stream content 上限截断（约 20480 字节）  
5. 自首次 stream 起有平台超时（文档约 10 分钟）——对齐 Gateway turn timeout（默认 120s）即可

本期不实现模板卡片交互 / `aibot_send_msg` 无触发推送（告警仍走 Portal Webhook）。

### 5.2 回复卡片约定（必须）

```text
发起人：{asker_name}
问题：{question_text}

{assistant_answer}
```

### 5.3 与 Portal Webhook 并存

| 路径 | 用途 |
|------|------|
| Gateway `wecom_bot` 长连接出站 | 回应当前对话 |
| Portal `wecom` Webhook | 工具/Cron **单向推群** |

---

## 6. 与现有代码落点

| 组件 | 动作 |
|------|------|
| `gateway/internal/wecom/wsclient.go` | WSS 连接、subscribe、ping、读写帧 |
| `gateway/internal/wecom/normalize.go` / `card.go` | 与消息体字段解析、卡片格式 |
| `gateway/internal/adapter/wecom_bot.go` | 按 channel 启停 client；回调 → Runtime → respond |
| `gateway/cmd/gateway/main.go` | 启动时挂载 wecom_bot runners（**无** `/hooks/wecom_bot` HTTP） |
| `gateway/configs/channels.yaml` | 示例 `wecom_bot` |
| Portal | **不改** `type=wecom` |

复用：`channel.Registry`、`session.Router`、`runtimeclient`、`idempotency.Store`。

---

## 7. 测试与验收

### 7.1 单测

- subscribe 帧格式 / 坏 secret 失败路径（mock WS）  
- `question_text` 剥离 `@机器人`  
- 卡片含发起人+问题  
- 同 `msgid` 幂等  
- 同 `chat:` peer 两轮同 session  
- turns 失败仍 `finish=true` 失败卡片  
- `req_id` 透传

### 7.2 验收清单

1. 控制台选长连接；Gateway 日志显示 subscribe 成功  
2. 单聊文本 → 收到「发起人/问题」回复  
3. 群 @ → `chat:` 续聊；问题正文正确（非 `@机器人 msgid`）  
4. Portal `send_to_wecom` 不变  
5. HITL 不自动放行  

---

## 8. 实现期锁定（见计划 Locked decisions）

1. 端点默认 `wss://openws.work.weixin.qq.com`；凭证 `bot_id` + `secret`。  
2. 消息字段同长连接文档 `aibot_msg_callback.body`（与 URL 模式明文结构一致）。  
3. `asker_name` 一期 = userid。  
4. 多副本：同一 `bot_id` 仅一台 Gateway `enabled: true`。  

---

## 9. 非目标回顾

- URL 回调 Adapter（可另开；与长连接互斥）  
- 自建应用 XML 回调  
- 迁 Portal 群 Webhook  
- 企微侧知识集 / 工作流 / MCP 产品配置  
- Webhook 完整 HITL  
