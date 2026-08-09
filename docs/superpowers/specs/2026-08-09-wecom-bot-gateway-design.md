# 企微智能机器人 Gateway 双向接入

**日期**: 2026-08-09  
**状态**: 设计已确认；待实现规划  
**目标**: 在已落地的入站 Gateway 上增加 **企业微信「智能机器人」** Adapter，支持群/单聊入站对话与应用侧出站回复；Portal 现有 **群 Webhook 出站**（`type=wecom` / `send_to_wecom`）保持不动。

**关联**:
- [入站 Gateway 设计](./2026-08-09-inbound-gateway-design.md)
- [企微群机器人出站（Portal）](../../../portal/docs/superpowers/specs/2026-06-04-wecom-bot-design.md)

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 企微产品 | **智能机器人 AI+**（非自建应用回调） |
| 架构模式 | **方案 2**：Gateway 管智能机器人双向；Portal 保留群 Webhook 只出站 |
| 入站 | 智能机器人回调 URL → Gateway → Runtime `resolve` + `turns(final)` |
| 出站（对话回复） | 智能机器人回复/发消息能力（实现期对照官方文档锁定具体 API） |
| 出站（告警/工具推群） | **仍用** Portal `type=wecom` Webhook + `send_to_wecom` |
| 会话 peer | 群：`chat:{chat_id}`；单聊：`user:{user_id}` |
| 回复卡片 | 必须回显 **发起人** + **问题正文**（去 @机器人） |
| 非目标（本期） | 自建应用 XML 回调；迁 Portal Webhook；HITL 企微按钮；知识集/工作流编排产品化（企微侧能力，不在本 Adapter 范围） |

---

## 1. 背景

截图场景（「小天才 BOT」）：

- 在群成员中以 **BOT** 出现，可主动发长文/结构化卡片（出站）
- 支持「引用并 @机器人 可继续」（入站续聊）
- 管理入口为企微 **智能机器人 AI+**（可单聊/群聊使用）

与一期 Gateway（`web` + 通用 `webhook`）的关系：智能机器人是 **第二个正式 IM Adapter**，复用 Registry / SessionRouter / RuntimeClient / 幂等，不新开进程。

与 Portal 群 Webhook 的关系：

| 能力 | 承载 |
|------|------|
| @续聊、对话闭环 | Gateway `wecom_bot` |
| Agent 工具单向推群、Cron 投递 | Portal `wecom` Webhook（不动） |

禁止把同一套凭证混用在两种 Channel 类型上。

---

## 2. 架构

```text
企微智能机器人回调
        │
        ▼
┌───────────────────────────────────────┐
│ Gateway                               │
│  GET/POST /hooks/wecom_bot/{channel_id}│
│  · URL 校验 / 签名                    │
│  · Normalize（asker + question_text） │
│  · peer → Runtime resolve             │
│  · turns(final)                       │
│  · Outbound：机器人回复 API           │
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
    # 以下字段名以实现期官方「智能机器人」文档为准；此处为逻辑必填项
    bot_id: "..."
    callback_token: "..."          # 回调校验
    callback_aes_key: "..."        # 若文档要求加密
    corp_id: "..."                 # 若发消息需要
    secret: "..."                  # 若换 access_token 需要
    default_reply_mode: async      # 回调先快速 ack，后台 turns 后再发回复
```

说明：

- 智能机器人控制台「创建机器人」后下发的回调地址应指向：  
  `https://<gateway-public>/hooks/wecom_bot/{channel_id}`
- 凭证只存在 Gateway 配置（渠道权威在 Gateway，与入站 Gateway 规格一致）

---

## 4. 入站流程

### 4.1 回调

1. **GET（若文档有 URL 验证）**：完成 echo/challenge，通过后企微才推送消息。
2. **POST**：验签（及解密，若需要）→ 解析事件。
3. 一期只处理 **文本**（含群 @）；图片/文件可后续扩展。
4. 在企微要求的时限内先返回成功 ack（`default_reply_mode=async`），再后台跑 Agent。

### 4.2 Normalize（关键）

从回调体抽出：

| 字段 | 含义 |
|------|------|
| `asker_id` | 提问者企微 user id |
| `asker_name` | 展示名（回调有则用；无则后续通讯录补全，一期可先显示 userid） |
| `question_text` | **问题正文**：去掉 `@机器人` / 纯提及壳后的自然语言 |
| `chat_id` | 群 id（群聊） |
| `msg_id` | 幂等键 |
| `quoted` | 若有引用，可附在 content 上下文，但 **不得替代 question_text** |

**禁止**把任务号、内部 handle、纯 `@小天才 xxx_id` 串当作 `question_text` 回显到卡片「问题」字段（反例：截图中「问题」被写成 `@小天才 9999_…`）。

注入 Runtime 的 user content 建议格式：

```text
[企微] 发起人={asker_name}({asker_id})
问题：{question_text}
```

### 4.3 会话

- 群：`peer_id = chat:{chat_id}` → 同群续聊同一 session  
- 单聊：`peer_id = user:{asker_id}`  
- `agent_id` = channel `default_agent`（已存在映射时忽略更换，遵循 Gateway peer 语义）

幂等：同 `msg_id` 不重复开 turn。

### 4.4 HITL

无企微确认交互面：危险确认类 **fail-closed**；出站一条「请到 Web 确认后继续」。

---

## 5. 出站（对话回复）

### 5.1 发送

- turns 完成后，用智能机器人官方 **回复/主动发消息** API 发回原会话（群或单聊）。
- 文本超长按平台限制切分。
- token（若需要）进程内缓存，过期重试一次。

### 5.2 回复卡片约定（必须）

出站正文（markdown/text）须包含：

```text
发起人：{asker_name}
问题：{question_text}

{assistant_answer}
```

可选扩展字段（与「问题」分离）：任务号、项目、能力版本等——**不得挤占「问题」**。

### 5.3 与 Portal Webhook 出站并存

| 路径 | 用途 |
|------|------|
| Gateway `wecom_bot` 出站 | 回应当前对话（谁问什么 → 答什么） |
| Portal `wecom` Webhook | Agent 工具/Cron **单向推群**，不参与 @续聊 peer |

---

## 6. 与现有代码落点

| 组件 | 动作 |
|------|------|
| `gateway/internal/adapter/wecom_bot.go` | 新建：校验、normalize、ack、调 outbound |
| `gateway/internal/wecom/`（可选包） | 加解密/签名/token（按文档） |
| `gateway/cmd/gateway/main.go` | 注册 `GET|POST /hooks/wecom_bot/{channel_id}` |
| `gateway/configs/channels.yaml` | 示例 `wecom_bot` 渠道 |
| Portal | **不改**现有 `type=wecom` 出站；不在本期迁配置 |

复用：`channel.Registry`、`session.Router`、`runtimeclient`、`idempotency.Store`、`reply`（若走 HTTP 回调式回复；否则专用 sender）。

---

## 7. 测试与验收

### 7.1 单测

- 签名错误拒绝  
- URL 验证成功路径  
- `question_text` 剥离 `@机器人`  
- 卡片渲染含发起人+问题  
- 同 `msg_id` 幂等  
- 同 `chat:` peer 两轮同 session  

### 7.2 验收清单

1. 智能机器人控制台回调地址指向 Gateway，验证通过  
2. 单聊文本 → 收到带「发起人/问题」的回复  
3. 群 @ 文本 → `chat:` peer 续聊；回复回显提问者与问题正文  
4. Portal `send_to_wecom` 群 Webhook 行为不变  
5. HITL 场景出站提示去 Web，不自动放行危险操作  

---

## 8. 实现期开放问题（写计划时收敛）

1. 智能机器人官方回调字段名、加密算法、同步回复 vs 异步发消息的最终选型（以当前企微文档为准）。  
2. 群聊 id / 单聊 user id 在回调 JSON/XML 中的准确路径。  
3. `asker_name` 是否需通讯录 API；一期是否允许先显示 userid。  
4. 公网暴露：Gateway 需对企微出口 IP 或签名强制校验；`reply_url` 式 SSRF 规则不适用时改用官方 host allowlist。  

---

## 9. 非目标回顾

- 自建应用（应用管理）XML 回调 Adapter（可另开规格）  
- 把 Portal 群 Webhook 迁入 Gateway  
- 企微侧「知识集 / 工作流编排 / MCP 插件」产品配置（属企微控制台，不由本 Adapter 实现）  
- Webhook 完整 HITL  
