# Gateway 消息级自动路由（@Agent + Portal 分类器）

**日期**: 2026-08-10  
**状态**: 设计已确认；待实现规划  
**目标**: 在一期「Portal 白名单 + 显式 `/agent` 改绑」之上，让普通业务消息可通过 `@Agent` 或轻量分类器自动选中白名单 Agent，并在换人时 `force_new` 新开 session 后直接进入本条 Turn。

**关联**:
- [Gateway / Portal 入站 Agent 路由与改绑](./2026-08-10-gateway-portal-agent-routing-design.md)（一期 B+C，本文件为二期 A）
- [入站 Gateway 设计](./2026-08-09-inbound-gateway-design.md)
- Gateway README：`gateway/README.md`

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 触发 | **@Agent 名/id** + **Portal 轻量分类器** |
| 优先级 | slash 指令 → @ 命中 →（无 @ 时）分类器 → 普通 Resolve |
| 换人语义 | 自动 **`force_new`**，**本条消息直接 Turn**（不先发确认卡） |
| 分类器落点 | **Portal**；仅当正文**没有**合法 @Agent 时调用 |
| @ 解析落点 | **Gateway**（候选来自 `ListChannelAgents`） |
| 架构 | Gateway 解析 @；无 @ 则调 `POST .../route`；再 Resolve + Turn |
| 失败策略 | 分类/超时 **fail-open**（续聊当前或 default，不强制换人） |

---

## 1. 问题

一期已支持：

- Portal `default_agent` + `allowed_agents`
- Resolve `force_new` / `AGENT_BOUND`
- 显式 `/agent`、`/new`、`/unbind`

缺口：用户必须先发指令才能换 Agent；自然语言或 `@某人` 无法自动进对的 Agent。

本设计补齐消息级自动路由，且不破坏一期契约。

---

## 2. 总流水线

```text
入站正文
  → slash 指令？ → 现有 /agent|/agents|/new|/unbind（本条不 turn）
  → 解析 @Agent（仅渠道白名单 name/id）
       命中 → strip mention
            → 若 agent ≠ 当前绑定则 Resolve(force_new=true, agent_id)
            → 否则普通 Resolve
            → Turn(stripped)
       未命中 → POST /runtime/v1/channels/{channel_id}/route { text, peer_id? }
                 → high 且换人 → Resolve(force_new=true) → Turn(原文)
                 → 否则 → 普通 Resolve → Turn(原文)
```

企微路径：先完成现有「去 @bot」规范化，再跑上述逻辑。

---

## 3. @ 解析规则（Gateway）

- 候选：`ListChannelAgents` 返回的 id + name（default∪allowed）。
- 支持：`@name`、`@id`（完整 UUID）；大小写不敏感；**最长 name 优先**匹配。
- 仅匹配本渠道候选；不查全站 Agent。
- 多个合法 @：取**第一个**；其余留在正文。
- strip：移除该 mention token 及紧邻多余空白，得到 Turn content。
- 非白名单 `@foo`：视为未命中，进入分类器或普通 Resolve。

---

## 4. `route` API（Portal）

`POST /runtime/v1/channels/{channel_id}/route`（service token）

```text
Request:  { "text": "...", "peer_id": "..." }  // peer_id 可选
Response: {
  "agent_id": "...",
  "confidence": "high" | "low",
  "source": "classifier" | "default" | "current",
  "reason": "..."
}
```

| 情况 | 行为 |
|------|------|
| 渠道不存在 | `404 CHANNEL_NOT_FOUND` |
| 白名单仅 1 个（或空名单仅 default） | 不调 LLM；返回该 agent，`confidence=high`，`source=default` |
| 分类成功且 high 且 ∈ 白名单 | 返回该 agent，`source=classifier` |
| 超时 / 坏 JSON / 非白名单 / low | fail-open：`confidence=low`，`agent_id=current||default`，`source=current` 或 `default` |

### 分类器

- 输入：user text + 候选 `{id,name,description截断}`  
- 输出：严格 JSON `{"agent_id":"...","confidence":"high"|"low"}`  
- 模型：Portal 已有 growth/aux LLM 配置；超时 **2–3s**；`temperature=0`；小 `max_tokens`  
- 仅 **high + 白名单** 可触发 Gateway 自动 `force_new`

### Gateway 使用

| route 结果 | 行为 |
|------------|------|
| high 且 ≠ 当前绑定 | `Resolve(force_new=true, agent_id)` → Turn(原文) |
| high 且 = 当前 | 普通 Resolve → Turn |
| low / current / default | 普通 Resolve → Turn |

不向企微用户展示分类过程；日志记录 `source/confidence/agent_id`。

---

## 5. 渠道级开关

Portal `channels` 增量（默认均为启用）：

| 字段 | 默认 | 含义 |
|------|------|------|
| `auto_route_enabled` | true | 总开关 |
| `auto_route_mention` | true | @Agent |
| `auto_route_classifier` | true | 无 @ 时分类 |

为减少 RTT，可将 flags 附带在 `GET /runtime/v1/channels/{id}/agents` 响应中。

管理 UI（最小）：渠道编辑页三个勾选。不做分类 prompt 编辑台。

`auto_route_enabled=false`：跳过 @ 自动路由与分类（slash / 显式 API 仍可用）。

---

## 6. 错误与降级

| 情况 | 行为 |
|------|------|
| route 5xx / 超时 | fail-open：普通 Resolve + Turn；warn 日志 |
| force_new Resolve 失败 | 短错误回复；不 turn |
| ListChannelAgents 失败 | 无法做 @；可跳过 @ 直接 route 或普通 Resolve（实现选：跳过 @，尝试 route；route 也失败则普通 Resolve） |

---

## 7. 测试要点

1. `@name` 命中 → strip → 换人 force_new → Turn 用 stripped  
2. `@uuid` 同等  
3. 非白名单 `@foo` → 不因 @ 短路  
4. 无 @ + classifier high 换人 → force_new + Turn 原文  
5. classifier low → 不 force_new  
6. classifier 超时 → fail-open  
7. `auto_route_enabled=false` → 无自动 @/分类  
8. slash `/agent` 仍优先于自动路由  
9. 单候选渠道不调 LLM  

---

## 8. 成功标准

- `@ops-agent 查告警` 无需先 `/agent` 即可进入正确 Agent（本条 turn）。  
- 无 @ 的自然语言在多 Agent 渠道可高置信自动改绑。  
- 分类故障不影响基础续聊。  

---

## 9. 非目标

- 关键词规则表  
- 多 @ 仲裁 / 加权  
- 向用户展示「正在分类…」  
- 路由到渠道白名单外的 Agent  
- 同一 session 原地换 `agent_id`（仍用 force_new 新开）  

---

## 10. 实现落点（供 planning）

| 区域 | 变更 |
|------|------|
| Portal channels | `auto_route_*` 字段 + UI |
| Portal runtime | `POST .../route` + AgentRouteClassifier |
| Portal agents list | 响应附带 routing flags |
| Gateway | @ 解析模块；webhook/wecom 接入 route；日志 |
| 测试 | Gateway @ strip；Portal classifier fake model；adapter 集成 |
