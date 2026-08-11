# 企微 `/switch` 两步 Agent 绑定（Gateway pending）

**日期**: 2026-08-11  
**状态**: 设计已确认；待实现规划  
**目标**: 在企微智能机器人上提供显式、可确认的 Agent 改绑：用户发送 `/switch` 查看可用 Agent（含当前绑定），在短时窗口内回复序号完成 `force_new` 绑定；比自动路由更精准，且本条不跑业务 Turn。

**关联**:
- [Gateway / Portal 入站 Agent 路由与改绑](./2026-08-10-gateway-portal-agent-routing-design.md)
- [消息级自动路由](./2026-08-10-gateway-message-auto-route-design.md)
- [企微智能机器人设计](./2026-08-09-wecom-bot-gateway-design.md)
- Gateway README：`gateway/README.md`

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 交互 | **两步**：`/switch` 列序号名单 → 下一条回 `1`/`2`/… 绑定 |
| 当前绑定 | 名单中标注 **当前** Agent；无映射时标明未绑定（将用 default） |
| 绑定成功 | 仅确认卡；**本条不 Turn**；下一条再聊业务 |
| 非法输入（窗口内） | 提示回复合法序号；**保留** pending；不 Turn |
| 超时 | **2 分钟**；过期后清 pending，该消息按普通入站处理 |
| 改绑语义 | 与 `/agent` 相同：`force_new` 新开 session |
| 范围 | **本轮仅 `wecom_bot`**；Webhook 预留同一 Store 接口不接线 |
| Pending 存储 | **Gateway 进程内内存**（单实例 / 长连接粘性可接受） |
| 与自动路由 | **共存**；存在未过期 pending 时优先拦截，不跑 `@`/分类器 |

不在本轮：企微模板卡片按钮、Portal 持久化 pending、多 Gateway 共享 pending、Webhook 接线。

---

## 1. 问题

已有能力：

- `/agents`：列出白名单（无 pending、无当前高亮约定）
- `/agent <名|id>`：一步改绑
- 消息级自动路由：`@Agent` + 分类器（可能误切）

缺口：企微场景下用户希望**显式浏览 + 用序号精准绑定**，并看见**当前绑了谁**；自动路由不能替代这种可控改绑。

---

## 2. 入站流水线（企微）

在现有 `HandleWecomMsgCallback` 中，于 slash 全量分发与 `prepareAutoRoute` **之前**插入 pending 处理（`/switch` 本身可作为 slash 的一种，但「纯数字消费」必须在 auto-route 之前）：

```text
Normalize（去 @机器人）
  → 若 pending(channel, peer) 存在且未过期：
       · 匹配 /^\s*(\d+)\s*$/ 且落在 1..N
            → force_new 改绑到 agents[n-1]
            → 确认卡 → 清 pending → return（不 Turn）
       · 其它非 slash 文本
            → 提示「请回复 1–N」→ return（保留 pending，不 Turn）
       · 再次 /switch
            → 刷新名单与 TTL → return
       · 其它 slash（/agent、/agents、/new、/unbind）
            → 清 pending → 走原指令逻辑
  → 否则若命令为 /switch：
       ListChannelAgents + 查当前 agent_id
       → 回复序号名单（标当前）→ Put pending(TTL=2m) → return（不 Turn）
  → 否则：现有 slash → prepareAutoRoute → Resolve → Turn
```

Pending **过期判定**：在处理下一条消息时若 `now > expires_at`，Delete 后视作无 pending，该条走普通路径。

**实现假设（文本渠道）**：本轮企微 bot 入站以文本为主。pending 窗口内若收到非文本（图片/文件/语音等），与非法文本相同：提示回复序号并保留 pending，不 Turn。用户发送 `/1` 视为 slash（清 pending 后走未知/原指令），**不**当作序号 `1`；选人须发纯数字 `1`。

---

## 3. 组件

### 3.1 `PendingSwitchStore`（Gateway）

建议路径：`gateway/internal/adapter/pendingswitch`（或 `gateway/internal/pendingswitch`）。

```text
Key: channel_id + "\0" + peer_id
Value: {
  Agents: []{ID, Name}   // 展示时的稳定快照（防列表变化导致序号错位）
  ExpiresAt: time
}
```

操作：`Get`（过期则删并返回 miss）、`Put`、`Delete`。进程内 `map` + `sync.Mutex`。

Webhook 本轮不调用；接口保持可注入，便于二期挂载。

### 3.2 命令

- 在 `gateway/internal/command` 增加 `KindSwitch`（触发词：`/switch`；大小写不敏感，与现有 slash 一致）。
- `/agents` 行为不变；可选在文案末尾提示「改绑请用 /switch」（非必须）。
- `/agent <名>` 成功路径与失败返回前：`Delete` pending。

### 3.3 当前绑定

优先：对 `channel+peer` 做一次不强制换人的解析/读映射，得到 `agent_id`，再与名单项匹配显示名称。

- 有映射：名单中对应项标注「← 当前」，文案头写 `当前：{name}`。
- 无映射：`当前：未绑定（下一条将使用 default）`。
- 查询失败：仍列出名单；`当前：未知`；**不阻断** `/switch`。

若现有 Gateway 仅能通过 `Resolve` 间接得知当前 agent：允许用 `Resolve(force_new=false)` 且不带新 `agent_id` 读取；避免因此创建多余 session 是实现约束（若 Resolve 会新建，则应改用 Portal 只读 binding API——实现计划中二选一，优先只读）。

### 3.4 改绑

复用 `switchChannelAgent` 同等语义：`Invalidate` → `Resolve(agent_id, force_new=true, reason=slash_switch)` → 再 `Invalidate`；确认文案与 `/agent` 同风格（含短 session id）。

---

## 4. 用户可见文案（示意）

**名单**

```text
请选择要绑定的 Agent（2 分钟内回复序号）：
当前：Ops Bot
1. Alpha
2. Ops Bot  ← 当前
3. RCA

回复数字即可切换；超时后请重新发送 /switch。
```

**成功**：`已切换到 Ops Bot（session …xxxxxxxx）`  
**非法序号 / 非数字**：`请回复 1–3 的序号，或发送 /switch 重新选择。`  
**名单拉取失败 / 空名单**：不写 pending；沿用现有渠道/Agent 错误或「暂无可用 Agent」。

---

## 5. 错误处理

| 情况 | 行为 |
|------|------|
| ListChannelAgents 失败 | 不 Put pending；用户可见错误 |
| 白名单空 | 不 Put pending；提示无可用 Agent |
| 当前绑定查询失败 | 仍可 `/switch`；当前显示「未知」 |
| 序号越界 / 非数字（未过期） | 保留 pending；提示；不 Turn |
| 改绑 403 / 不在白名单 | 清 pending；白名单错误文案 |
| 改绑 5xx / 其它失败 | **清 pending**；提示重试并建议重新 `/switch` |
| Gateway 重启 | pending 丢失；用户再发 `/switch` |
| 多实例 | 不保证跨实例；本轮接受 |

---

## 6. 测试要点

1. `/switch` → 列表含当前标记；pending 写入；无 Turn  
2. 回复合法序号 → `force_new` 改绑 + 确认 + pending 清除 + 无 Turn  
3. 窗口内 `99` / `hello` → 提示、无 Turn、pending 仍在  
4. 过期后业务句 → 不拦截，走 Resolve/Turn（可走自动路由）  
5. pending 中 `/unbind`（或 `/agent`）→ 清 pending 并执行原指令  
6. 连续两次 `/switch` → 名单与 TTL 刷新  

---

## 7. 非目标与后续

- 不替换自动路由；`/switch` 是精准改绑通道。  
- 不实现企微交互式按钮卡片。  
- 二期可选：Portal 持久化 pending、Webhook 接线、多实例共享。

---

## 8. 成功标准

- 企微用户能通过 `/switch` + 序号完成精准绑定，并看到当前 Agent。  
- 待选窗口内误输入不会误触发 Turn 或自动路由。  
- 超时后行为与「从未 /switch」一致。  
- 现有 `/agent`、`/agents`、`/new`、`/unbind` 与自动路由回归通过。
