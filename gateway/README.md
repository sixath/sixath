# Sixath Inbound Gateway

入站 Gateway：Web 会话、`webhook` 渠道，以及企业微信 **智能机器人 AI+** 的 **WebSocket 长连接**（`type=wecom_bot`）。进程配置见 `configs/config.example.yaml`；渠道列表见 `configs/channels.yaml`。

设计规格：[`docs/superpowers/specs/2026-08-09-wecom-bot-gateway-design.md`](../docs/superpowers/specs/2026-08-09-wecom-bot-gateway-design.md)。官方文档：[智能机器人长连接](https://developer.work.weixin.qq.com/document/path/101463)。

## 本地运行

```bash
cd gateway
go run ./cmd/gateway -config ./configs/config.example.yaml
```

默认监听 `:8088`，通过 `portal_base_url` 与 `runtime_token` 调用 Portal Runtime。`channels_file` 指向 `./configs/channels.yaml`。

## 企微智能机器人（长连接 `wecom_bot`）

长连接模式下 Gateway **主动连出** 企微 WSS，**不需要**公网 HTTPS 回调 URL，也**不要**配置 URL 模式用的 `callback_token` / `EncodingAESKey`。

### 1. 控制台配置

在企业微信管理后台进入 **智能机器人 AI+** → **API 配置**：

1. 接入方式选择 **「使用长连接」**（与「接收消息 URL 回调」互斥；切换模式会踢掉旧连接或旧回调）。
2. 复制 **BotID**。
3. 获取 **Secret**（长连接专用，通常只展示一次；丢失需在控制台重新生成并更新 Gateway 配置）。

将 BotID 与 Secret 写入 Gateway 的 `channels.yaml`（见下），**不要**提交到 Git。

### 2. `channels.yaml` 字段（`type: wecom_bot`）

| 字段 | 说明 |
|------|------|
| `id` | Gateway 内渠道唯一标识（自定义，如 `xiaotiancai`）。 |
| `type` | 固定为 `wecom_bot`。 |
| `enabled` | `true` 时启动 WS 客户端并 `aibot_subscribe`；占位示例为 `false`。 |
| `default_agent` | 该渠道会话绑定的 Agent UUID（Runtime `resolve` 使用）。 |
| `bot_id` | 控制台 **BotID**。 |
| `secret` | 控制台 **长连接 Secret**（≠ URL 回调 Token）。 |
| `bot_names` | 可选。机器人在群里的显示名列表，用于从文本中剥离 `@提及`（如 `["小天才"]`）。 |
| `ws_url` | 可选。默认 `wss://openws.work.weixin.qq.com`；一般无需修改。 |
| `corp_id` | 可选。企业 ID；与 `corp_secret` 一起用于把 `from.userid` 解析成通讯录姓名（「发起人」展示）。 |
| `corp_secret` | 可选。自建应用 Secret（需成员读取权限）；**不要**提交到 Git。未配置时「发起人」回退为 userid。 |

示例（启用前请填入真实凭证）：

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
    corp_id: "<企业 CorpID>"
    corp_secret: "<自建应用 Secret>"
```

`enabled: true` 时 `bot_id` 与 `secret` 必填，否则 Gateway 启动加载 channels 会失败。
`corp_id` / `corp_secret` 可选：配置后 Gateway 会调用 `gettoken` →（必要时）`openuserid_to_userid` → `user/get`，将「发起人」显示为 `别名(姓名)`；解析失败不阻塞回复。

### 3. 单副本约束（重要）

企微对同一 BotID **同时只允许一条有效长连接**。新的 `aibot_subscribe` 会踢掉旧连接。

因此：

- **同一个 `bot_id` 不要在多台 Gateway 实例上同时 `enabled: true`**（包括 K8s 多副本、蓝绿两套进程）。
- 生产上应保证「一个 bot → 一个 Gateway 进程/实例」跑该 channel，或由外部选主后再启用。

若多实例重复订阅，会表现为频繁断线重连（日志中出现 `wecom_bot <id> disconnected`）或消息处理不稳定。

### 4. 与 Portal `type=wecom` Webhook 凭证分离

| 能力 | 承载 | 凭证 |
|------|------|------|
| 群/单聊 @ 续聊、对话回复 | Gateway `wecom_bot` 长连接 | `bot_id` + `secret` |
| Agent 工具/Cron **单向推群** | Portal Channel `type=wecom` Webhook | Webhook URL 等（见 Portal 文档） |

两套凭证**不可混用**。Portal 的群机器人 Webhook **不需要**改；告警与工具推群仍走 `send_to_wecom`。

### 5. 验收清单

配置正确且 Gateway 与 Portal 可达时：

1. **订阅成功**：启动后连接保持稳定，不出现 `wecom subscribe rejected`；不应立刻进入 `wecom_bot … disconnected` 重连循环（凭证错误或重复订阅时会失败）。
2. **单聊**：发送文本 → 收到 stream 回复卡片，正文包含 **发起人**、**问题** 与 Agent 回答。
3. **群聊**：@ 机器人提问 → 同上；会话按 `chat:{chat_id}` 续聊；「问题」应为去掉 @ 后的正文，而非原始 `@机器人 msgid` 噪声。

回复卡片格式约定：

```text
发起人：{asker_name}
问题：{question_text}

{assistant_answer}
```

失败时仍会 `finish=true` 出站失败卡片（含发起人/问题），不会静默丢弃。

## 其它渠道

- **`webhook`**：`POST /hooks/{channel_id}`，见仓库内 E2E 脚本 `_neo4j_q/verify_inbound_gateway.ps1`。
- **Web**：经 Portal 鉴权的浏览器会话入口（与 `wecom_bot` 并行，共享 Session / 幂等存储）。
