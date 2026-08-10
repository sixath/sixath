# Gateway / Portal 入站 Agent 路由与改绑

**日期**: 2026-08-10  
**状态**: 设计已确认；待实现规划  
**目标**: 打破「渠道 default_agent + channel+peer 映射后永不换绑」的僵硬模型；一期交付多 Agent 白名单与显式改绑（新开 session），配置权威在 Portal。

**关联**:
- [入站 Gateway 设计](./2026-08-09-inbound-gateway-design.md)
- [企微智能机器人 Gateway](./2026-08-09-wecom-bot-gateway-design.md)
- Gateway README：`gateway/README.md`

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 长期诉求 | 动态换 Agent（A）+ 渠道多 Agent（B）+ 改绑/解绑/新开（C） |
| **一期范围** | **B + C**；消息级自动路由（A）二期 |
| 改绑会话语义 | **新开 session**；旧 session 保留；peer 映射切到新 session |
| 选择入口 | **指令**（真人渠道）+ **API**（系统集成） |
| Agent 目录权威 | **Portal**（即时生效）；不靠改 `channels.yaml` 换 Agent |
| `channels.yaml` | 仅协议/密钥（bot_id、secret、IP 等）；**废弃**其中的 `default_agent` 作为真相源 |
| 架构方案 | Portal 扩展 `channels` + Resolve 契约；Gateway 翻译指令并调 Runtime |

---

## 1. 问题

现状（Webhook / 企微等经 Gateway）：

1. Gateway `channels.yaml` 配单一 `default_agent`；改文件需**重启** Gateway。
2. Portal `ChannelPeerUsecase.Resolve`：若 `channel+peer` 映射已存在，**忽略**请求中的新 `agent_id`，无法改绑。
3. 同一渠道无法声明多个可选 Agent；用户/调用方无法显式切换。

需要：

1. Portal 持有每渠道 `default_agent` + `allowed_agents`，可 API/UI 修改且立即对下一请求生效；
2. Resolve 支持强制新开并更新映射（改绑）；
3. Gateway 提供 `/agent` `/new` `/unbind` `/agents`，以及 hook/API 传 `agent_id`/`force_new`。

---

## 2. 权威边界

| 数据 | 权威 | 说明 |
|------|------|------|
| 协议密钥、`bot_id`、WSS、webhook_secret、IP 白名单 | Gateway `channels.yaml` | 仍可能需重启；与 Agent 解耦 |
| `channel_id` → `default_agent` + `allowed_agents[]` | **Portal `channels` 表** | 即时生效 |
| `channel+peer → session` | Portal `channel_peer_sessions` | 改绑时 upsert 映射 |

Gateway 的入站 `channel_id`（如 `sixath4`）必须与 Portal `channels.channel_id` 对齐；Portal 无记录则 Resolve **fail-closed**（`CHANNEL_NOT_FOUND`）。

不新建平行的 `gateway_channel` 表，避免双份渠道真相。

---

## 3. 数据模型（Portal）

复用现有 `channels` 表，一期增量：

| 字段 | 类型 | 语义 |
|------|------|------|
| `allowed_agents` | JSON string[] | 允许绑定的 Agent UUID 列表 |
| `default_agent` | 已有 | 未指定 `agent_id` 时使用 |

规则：

- **`allowed_agents` 为空**：仅允许 `default_agent`。
- **非空**：请求的 `agent_id`（及 default）必须 ∈ 列表；创建/更新渠道时校验 `default_agent ∈ allowed_agents`（或写入时自动把 default 纳入列表）。
- Agent 必须存在；不存在则管理 API / Resolve 失败。

迁移：为现有行设 `allowed_agents = []`（兼容「仅 default」）；运维将 Gateway yaml 中的 channel_id/default 同步进 Portal（文档 + 可选一次性脚本，非运行时双写）。

---

## 4. Resolve / 改绑语义

接口：`POST /runtime/v1/sessions/resolve`（service token）。

### 4.1 请求增量

```text
{
  "channel_id": "...",
  "peer_id": "...",
  "agent_id": "...",      // 可选；空则用 Portal channel.default_agent
  "force_new": false,     // true = 强制新开 session 并改映射
  "reason": "rebind"      // 可选审计：rebind | new | unbind_reset
}
```

### 4.2 决策表

先将「解析后 agent_id」定为：请求 `agent_id` 非空则用之，否则 `default_agent`。再：

| 已有映射？ | force_new | 解析后 agent | 行为 |
|-----------|-----------|--------------|------|
| 否 | * | 合法 | **创建** session + 写入映射；`created=true` |
| 是 | false | 与映射相同 | **续聊**；`created=false` |
| 是 | false | 与映射不同 | **`409 AGENT_BOUND`**（禁止静默换人） |
| 是 | true | 合法 | **新开** session；upsert 映射；旧 session **保留**；`created=true` |
| * | * | 不在白名单 | **`403 AGENT_NOT_ALLOWED`** |
| * | * | Portal 无 channel | **`404 CHANNEL_NOT_FOUND`** |
| * | * | Agent 不存在 | **`404 AGENT_NOT_FOUND`** |

### 4.3 改绑步骤（force_new=true）

1. 用解析后 `agent_id` 创建 chat session（`user_id` 仍用现有 `PeerUserID(channel, peer)`）。
2. Upsert `channel_peer_sessions`：`session_id`、`agent_id`、`updated_at`。
3. 旧 session 不删除；若已有 `readonly` 能力可标记（非硬依赖）；Web 仍可按旧 `session_id` 打开历史。

### 4.4 解绑

Gateway `/unbind` 调用 Runtime：

`DELETE /runtime/v1/sessions/binding?channel_id=&peer_id=`（service token）

删除该 `channel+peer` 映射行，**不**删历史 session。下一句普通消息按 default **新建**。无映射时幂等成功。

### 4.5 Gateway 缓存

SessionRouter 短缓存：`force_new` 成功、解绑成功、或收到需换绑的错误处理后，**失效**该 `channel+peer` 缓存项。

---

## 5. 入口面

### 5.1 真人渠道指令（Gateway，turn 前拦截）

| 指令 | 行为 |
|------|------|
| `/agent <id\|name>` | `force_new=true` + 解析目标 Agent；成功短确认；**本条不跑业务 turn** |
| `/agent` 或 `/agents` | 列出本渠道 default + allowed（Portal 只读）；不改绑 |
| `/new` | `force_new=true`，agent=当前映射或 default；新会话；不跑业务 turn |
| `/unbind` | 清映射；提示下句将按 default 新建 |

无法识别的 `/…`：短错误，不落 turn。  
`/agent <name>` **仅**在本渠道 default∪allowed 内解析，防枚举全站 Agent。

### 5.2 系统集成

- Runtime Resolve：可直接传 `agent_id` + `force_new`。
- Webhook body 可选 `agent_id` / `force_new`（与指令等价）；**白名单仍由 Portal 校验**。
- 管理 API（Portal `/api/v1`）：渠道 GET/PATCH 读写 `default_agent`、`allowed_agents`。
- 可选：`GET /runtime/v1/channels/{channel_id}/agents` 供 Gateway `/agents`（service token）。

### 5.3 管理 UI（一期）

Portal 渠道编辑：多选 allowed Agents + 指定 default。  
**不做**：Gateway 管理台、关键词/自动路由。

### 5.4 yaml 迁移

文档明确：`channels.yaml` 的 `default_agent` **废弃为真相源**；每个入站 channel_id 须在 Portal 配置。Gateway 遇 `CHANNEL_NOT_FOUND` 时日志提示配置 Portal。

---

## 6. 错误处理

| Portal 码 | 渠道侧文案要点 |
|-----------|----------------|
| `CHANNEL_NOT_FOUND` | 渠道未在 Portal 配置 |
| `AGENT_NOT_ALLOWED` | 不在本渠道白名单 |
| `AGENT_BOUND` | 已绑定其它 Agent；提示 `/agent` 或 `/new` |
| `AGENT_NOT_FOUND` | Agent 不存在或名称无法解析 |
| 5xx | 稍后重试；日志含 channel/peer |

指令成功：短确认；避免刷屏完整 UUID（可附短后缀排障）。

---

## 7. 测试要点

1. 无映射 + 合法 agent → 创建  
2. 有映射 + 同 agent → 续聊  
3. 有映射 + 异 agent + `force_new=false` → `AGENT_BOUND`  
4. `force_new=true` → 新 session、映射更新、旧 session 可读  
5. agent ∉ allowed → `403`  
6. 空 allowed → 仅 default  
7. Gateway 指令不误触发业务 turn  
8. Resolve/解绑后 peer 缓存失效  

---

## 8. 成功标准（一期）

- 改 Portal 白名单/default **无需重启 Gateway** 即可对下一请求生效。  
- 企微/Webhook 可用指令改绑到白名单内另一 Agent（新会话）。  
- 集成方可 API `force_new` 完成同等改绑。  

---

## 9. 非目标（一期）

- 消息级自动路由（关键词 / @技能选 Agent）  
- `channels.yaml` 热加载  
- 同一 session 原地换 `agent_id`  
- 跨渠道共享 peer 映射  
- 完整 Gateway 配置管理台  

---

## 10. 实现落点（供 planning）

| 区域 | 变更 |
|------|------|
| Portal `channels` / migration | `allowed_agents`；校验 default∈allowed |
| Portal `ChannelPeerUsecase.Resolve` | 白名单、`force_new`、upsert 映射；冲突码 |
| Portal Runtime HTTP | Resolve body 字段；可选 list agents；unbind |
| Portal 管理 API + Web 渠道表单 | 编辑 allowed / default |
| Gateway adapters | 指令解析；hook 可选字段；错误文案；缓存失效 |
| Gateway `channels.yaml` / 文档 | 废弃 default_agent 真相源；对齐 Portal channel_id |
| 测试 | biz Resolve 表驱动 + Gateway 指令单测 + 文档 E2E 要点 |
