# wecom_bot 企微卡片进度反馈

**日期**: 2026-08-11  
**状态**: 设计已确认，待实现  
**目标**: 将企微智能机器人 stream 卡片从固定「处理中…」升级为可刷新的进度文案（耗时 / 阶段 / 当前工具 / 已完成步数），固定每 5 秒刷新同一条卡片；Turn 结束后整卡换成最终答案或失败卡。

**关联**:
- [企微智能机器人设计](./2026-08-09-wecom-bot-gateway-design.md)
- 实现入口：`gateway/internal/adapter/wecom_bot.go`
- Portal SSE：`portal/internal/chatsse/sse.go`（`tool_call` / `model_call` / `chunk` / `error` / `done` 等）

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 范围 | **仅 `wecom_bot`** |
| 展示字段 | 耗时、当前阶段、当前工具名、已完成步骤数 |
| 刷新策略 | **固定每 5 秒**（另：首帧立刻推骨架卡） |
| 实现路径 | **Gateway 聚合 Portal SSE + 本地 ticker 推卡** |
| 终卡 | **整卡换成** `FormatReplyCard` / `FormatFailureCard`（进度文案消失） |
| 刷新间隔 | 本期写死 **5s**，不可配置 |

不在本轮：Web/其他渠道进度 UI、Portal 新 `progress` API、企微真侧栏/多卡、终卡内嵌完整时间线、可配置刷新间隔。

---

## 1. 问题

现状（`HandleWecomMsgCallback`）：

1. 收到消息后立刻 `RespondStream(..., "处理中…", finish=false)`
2. 使用 `Runtime.TurnsFinal` 阻塞至 Turn 结束
3. 再用 `finish=true` 写入最终回复或失败卡

长耗时 Turn（多工具 / 多轮模型调用）期间，用户只能看到静态「处理中…」，无法判断是否在推进、卡在哪一步。

企微 `aibot_respond_msg` stream 只能刷新**同一条**卡片，没有独立右侧进度栏；进度必须以同一 `streamID` 的文案更新呈现。

---

## 2. 架构

```text
企微消息
   │
   ▼
wecom_bot.HandleWecomMsgCallback
   │  快路径（斜杠 / PendingSwitch 等）──▶ 即时终卡（不启 ticker）
   │
   ▼  长 Turn
TurnsStream (Portal SSE)
   │
   ├─▶ 解析事件 ──▶ 更新 ProgressState（仅内存）
   │
   └─▶ 5s ticker ──▶ FormatProgressText ──▶ RespondStream(finish=false)
                                              │
Turn 终态（done / error / HITL / ctx）────────┴─▶ 停 ticker
                                              RespondStream(finish=true) 终卡
```

- **Portal**：不变；继续发出既有 SSE 事件。
- **Gateway `wecom_bot`**：从 `TurnsFinal` 改为消费 `TurnsStream`；本地维护进度并节流推卡。
- **Web / 其它 adapter**：不改。

---

## 3. ProgressState 与文案

### 3.1 状态字段

| 字段 | 含义 |
|------|------|
| `startedAt` | Turn 开始时间（Gateway 本地） |
| `stage` | `思考中` / `调用工具` / `生成回复` |
| `toolName` | 最近一次进行中或刚开始的工具名；无则展示 `—` |
| `stepsDone` | 已完成步数（见映射规则） |
| `lastError` | 可选；用于失败终卡（不进进度文案） |

### 3.2 SSE → 状态映射

| SSE 事件 | 状态更新 |
|----------|----------|
| `model_call` `phase=invoked` | `stage=思考中`（若当前 `stage=调用工具` 且该 tool 尚未 `completed`，保持「调用工具」） |
| `tool_call` `phase=started` | `stage=调用工具`；`toolName=tool_name` |
| `tool_call` `phase=completed`（含错误完成） | `stepsDone++`（按 tool `id` 去重）；**保留** `toolName` 为最近工具名 |
| `model_call` `phase=responded` | `stepsDone++`（按 model `step` 去重）；`stage` 保持 `思考中`（除非已是「生成回复」） |
| `chunk`（首次非空正文） | `stage=生成回复` |
| `error` / HITL（`input_required` / `confirm_required`） | 记失败语义，准备终卡（与今日 `AggregateFinal` 非交互面一致） |
| `done` | 停止进度循环；**不**依赖 `done` 上的 `status`/`content`（Portal `done` 常无正文） |

终态判定（stream）：

- 出现 `error` 或 HITL → **失败卡**
- 否则聚合全部 `chunk` 正文 → **成功卡**（正文为空且无显式错误时，按现有失败文案兜底）

`stepsDone` 定义：**每完成一次 tool（`completed`）或一次 model（`responded`）计 1**；同一 tool `id` / 同一 model `step` 不得重复计数。

### 3.3 进度文案格式

纯文本，示例：

```text
处理中…
耗时 00:42
阶段 调用工具
工具 kubectl_logs
已完成 2 步
```

- 耗时：`mm:ss`，由 `time.Since(startedAt)` 计算（ticker 触发时刷新）。
- 首帧：立刻推骨架（耗时 `00:00`，阶段 `思考中`，工具 `—`，已完成 `0 步`）。

### 3.4 终卡

成功：`wecom.FormatReplyCard(asker, question, content)`，`finish=true`。  
失败 / 超时 / HITL：`wecom.FormatFailureCard(...)`，`finish=true`。  
**终卡不再包含进度区块。**

---

## 4. 推卡与并发

1. **首帧**：进入长 Turn 后立刻 `RespondStream(finish=false)` 骨架卡（可替换今日单行「处理中…」）。
2. **SSE 路径**：只更新 `ProgressState`，**不**立即推卡。
3. **Ticker**：`time.NewTicker(5 * time.Second)`；每次用最新状态格式化后 `RespondStream(finish=false)`。
4. **终态**：先 `Stop` ticker，再推终卡；保证终卡之后不再有进度推送。
5. **中间推卡失败**：打日志，继续收 SSE；终卡再试一次。
6. **ctx 取消 / TurnTimeout**：停 ticker → 失败卡。

快路径（PendingSwitch、斜杠命令、Resolve 失败等）：不启 stream / ticker。今日回调开头会先推一帧「处理中…」再走快路径终卡；**本轮保持该行为**（不顺带去掉快路径上的 processing 帧），避免范围膨胀。

实现时注意：`TurnsStream` 必须使用**不带整体 Timeout 的 stream HTTP client**（与 Web 代理长 SSE 的修复一致），Turn 上限仍由现有 `TurnTimeout`/`context.WithTimeout` 控制。

---

## 5. 组件边界（建议落点）

为保持 `wecom_bot.go` 可测、可读，建议拆出（名称可微调）：

| 单元 | 职责 |
|------|------|
| `ProgressState` + `ApplySSE(...)` | 事件 → 状态；纯函数/结构体，单测覆盖映射与去重 |
| `FormatProgressText(state)` | 状态 → 文案 |
| `runWecomTurnWithProgress(...)` | 启动 ticker、读 stream、聚合最终 content、停 ticker、返回结果或错误 |

`HandleWecomMsgCallback` 在 Resolve 成功后调用 `runWecomTurnWithProgress`，替代直接 `TurnsFinal`。

---

## 6. 错误与 HITL

| 情况 | 行为 |
|------|------|
| Portal/网络错误 | 失败卡（`mapRuntimeUserError`） |
| SSE `error` | 失败卡 |
| `input_required` / `confirm_required` | 非交互面视为失败并终卡（与现 `AggregateFinal` 语义对齐） |
| 流正常 `done` 但聚合正文为空 | 失败卡（兜底文案） |
| 中间进度推送失败 | 日志；不中断 Turn |

---

## 7. 测试

Gateway 单测（优先，假时钟 / 假 `WecomConn`）：

1. `FormatProgressText` 字段与格式
2. SSE 序列 → `ProgressState`（阶段、工具名、`stepsDone` 去重）
3. 假时钟：约 5s 推一次进度，**非**每个 SSE 事件推一次；首帧立即推
4. `done` → 终卡 content 为答案，且不含「耗时 / 阶段」进度块
5. 失败 / 超时 / HITL → 失败卡
6. 斜杠等快路径：无 ticker、行为回归

不要求本轮企微 Live E2E 作为合并门槛；可选手工：长工具 Turn 观察卡片每 ~5s 更新，结束后为完整答案。

---

## 8. 非目标

- Web 聊天时间线 / 其它渠道进度卡片
- Portal 新增专用 progress 事件或 API
- 企微多卡片或真·侧栏 UI
- 终卡底部保留执行摘要（已明确否决）
- 可配置刷新间隔
- 事件驱动即时推卡（方案 3）

---

## 9. 实现顺序（规划提示）

1. `ProgressState` + 文案 + 单测  
2. SSE 解析/应用 + 单测  
3. `runWecomTurnWithProgress`（ticker + `TurnsStream`）接 `HandleWecomMsgCallback`  
4. 失败/HITL/快路径回归测试  
5. 本地对真实企微可选验证
