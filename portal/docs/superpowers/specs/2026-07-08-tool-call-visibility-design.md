# Agent 工具/模型调用可视化设计

- 日期：2026-07-08
- 状态：设计已批准，待实现
- 涉及：`framework`（少量）、`portal`（主要）、`web`（主要）

## 背景与问题

当前 chat 页面是"黑盒"：用户只能看到 assistant 的最终文本，看不到本轮 Agent 调用了哪些工具、哪个模型、参数与结果是什么。出问题排查、做优化时非常不便。

现状梳理（关键事实，实现时以代码为准）：

- **框架层已具备实时结构化事件流**：`ReActAgent.RunEvents(ctx, req) (<-chan StreamEvent, error)` 已完整实现（`framework/agent/react_agent.go:387`），在执行过程中 `send`：
  - `StreamEventDelta`（文本增量）
  - `StreamEventToolStarted`（带 `*ToolCallRecord`，工具开始）
  - `StreamEventToolCompleted` / `StreamEventToolFailed`（带填充好的 `record`）
  - `StreamEventError`、`StreamEventDone`
- **`ToolCallRecord` 字段完整**（`executeOneToolCall` 已填充）：`ToolName`、`Arguments`、`Result`、`Error`、`Allowed`、`Decision`、`DurationMS`、`Step`、`ToolCallID`。
- **模型事件走事件总线**：`RunEvents` 内部同时 `emit` `ModelInvoked` / `ModelResponded` 到 `events.Bus`，但当前 payload 只有 `text_length` / `step` / `mode`，**缺 token 与模型名**。`model.Generation` 结构体已有 `TokenUsage *TokenUsage` 字段，数据源存在。
- **Portal 当前只流式吐文本**：`SendMessageStream`（`portal/internal/service/chat.go:366`）调用非流式的 `a.Run()`，只把最终 `resp.Text` 作为 `chunk` SSE 事件推给前端。仅在 `DebugRun` 模式下订阅事件总线，把原始 `kind[json]` 拼成字符串作为 `debug` 事件转发。
- **SSE 通道**：`portal/internal/server/chat_sse.go` 已有 `chunk`/`error`/`done`/`confirm_required`/`input_required`/`debug` 事件。前端在 `web/src/api/client.ts` 的 SSE 解析里消费。

## 目标

让 chat 页面像 Claude Code 一样，实时、结构化地展示本轮的**执行时间线**：模型推理节点 + 工具调用节点，可展开查看入参/结果/元数据。

非目标（YAGNI）：

- 不覆盖非流式 `a.Run()` 调用方。
- 不做"查看完整结果"的按需回拉（本期截断即可）。
- 不移除现有原始 `debug` 面板（作为开发者深挖入口保留）。

## 方案选型

采用**方案 2 —— 接通 `RunEvents` 流式接口**：portal 用 `a.RunEvents()` 替换 `a.Run()`，消费类型安全的 `StreamEvent`（其 `ToolCall` 即完整 `ToolCallRecord`）。

模型节点数据采用 **channel 为主 + 事件总线补模型信息**：`RunEvents` channel 提供工具节点与文本；portal 同时订阅 `events.Bus` 拿 `ModelInvoked`/`ModelResponded` 补出模型推理节点（模型名、token）。两个来源按 `RequestID` + 到达顺序合并成一条时间线。

## 架构与数据流

```
框架 ReActAgent.RunEvents()  ──channel──►  Portal ChatService  ──SSE──►  Web ChatPage
   (已实现，仅补模型 emit 字段)        (改造)           (新事件类型)        (时间线渲染)
        │
        └─ emit 到 events.Bus (ModelInvoked/ModelResponded 补 token/model)
                                    │
                                    └──► Portal 订阅(按 RequestID 过滤) ──► 合并进同一 SSE 流
```

三条来源合并成一条时间线（排序键：`step` 升序，同 step 内按到达顺序）：

1. channel `StreamEventToolStarted/Completed/Failed` → 工具节点（含完整 `ToolCallRecord`）
2. channel `StreamEventDelta` → 助手文本（照旧作为 `chunk`）
3. bus `ModelInvoked/ModelResponded`（按 `RequestID` 过滤）→ 🧠 模型推理节点

## 事件协议（SSE 契约）

现有 `chunk`/`error`/`done`/`confirm_required`/`input_required`/`debug` 保持不变。新增两种事件类型。

### `event: tool_call`

同一个 `id` 会推送多次以更新状态（started → completed/failed）。

```json
{
  "id": "call_abc123",
  "step": 2,
  "phase": "started|completed|failed",
  "tool_name": "execute_query",
  "arguments": { "sql": "SELECT …", "limit": 100 },
  "result": { "rows": 42 },
  "error": "",
  "allowed": true,
  "decision": "allowed",
  "duration_ms": 128,
  "truncated": false
}
```

- `arguments` 在 `started` 阶段即带上；`result`/`duration_ms` 在 `completed` 阶段带上；`error` 在 `failed` 阶段带上。
- 字段直接来自框架已填充的 `ToolCallRecord`，portal 只做映射，不重新组装。

### `event: model_call`

```json
{
  "step": 2,
  "phase": "invoked|responded",
  "mode": "tools",
  "model": "gpt-4o",
  "prompt_tokens": 320,
  "completion_tokens": 210,
  "message_count": 12
}
```

- `model` / `prompt_tokens` / `completion_tokens` 需在框架 `emit` 中补充（来自 `gen.TokenUsage` 与模型配置）。

### 状态机

前端按 `id`（工具）或 `step`+kind（模型）+ `phase` 做状态机：

- 收到 `started`/`invoked`：新建"进行中"节点。
- 收到 `completed`/`responded`/`failed`：原地更新为终态（补 `result`/`error`/`duration_ms`/token）。

### 截断策略

`result` 与 `arguments` 可能很大。portal **在映射时**对每个字段做截断（单字段上限 8KB，超出置 `truncated: true`），避免撑爆 SSE、卡顿前端。完整结果仍在结束后的 `RunTrace` 中留存。

## 框架侧改动（最小）

唯一改动：给 `ModelInvoked` / `ModelResponded` 的 `emit` payload 补 `model`、`prompt_tokens`、`completion_tokens`（从 `gen.TokenUsage` 取；模型名从 agent 的 model 配置取）。工具事件 payload 已完整，无需改。

涉及 `framework/agent/react_agent.go` 中各处 `emit(events.ModelResponded, …)` / `emit(events.ModelInvoked, …)` 调用点。

## Portal 侧改动（主要）

`portal/internal/service/chat.go` `SendMessageStream`：

1. 将 `a.Run()` 替换为 `a.RunEvents()`（类型断言 `agent.EventStreamableAgent`；若断言失败则回退现有 `a.Run()` 路径，保证兼容）。
2. 遍历 channel：
   - `StreamEventDelta` → 现有 `ChatStreamEventChunk`（文本累积/落库逻辑不变）。
   - `StreamEventToolStarted/Completed/Failed` → 新增 `ChatStreamEvent` 类型（携带映射后的 tool_call payload，含截断）。
   - `StreamEventError`/`StreamEventDone` → 现有 error/done 逻辑。
3. **始终**订阅 `events.Bus`（不再局限于 `DebugRun`），按 `RequestID` 过滤，将 `ModelInvoked`/`ModelResponded` 映射成新增的 model_call `ChatStreamEvent`。
   - 复用现有 `DebugRun` 的生命周期模式：`done` channel + `relayWg.Wait()` + channel 关闭后 `SetDefaultBus(prev)`，防止 goroutine 泄漏与跨轮串事件。
4. 现有 `DebugRun` 原始 debug 转发**保留**，与新结构化事件并存。

新增 `ChatStreamEventType`：`tool_call`、`model_call`。对应结构体承载上述 payload 字段。

`portal/internal/server/chat_sse.go`：`switch` 里新增 `case service.ChatStreamEventToolCall` / `ChatStreamEventModelCall`，`writeSSEEvent("tool_call", …)` / `writeSSEEvent("model_call", …)`。

## Web 侧改动（主要）

### SSE 消费（`web/src/api/client.ts`）

现有 `onChunk`/`onDebug`/`onConfirm`/`onInput` 之外，新增 `onToolCall`/`onModelCall` 回调，解析对应 `data`。

### 数据模型（`ChatPage.tsx`）

每条 assistant 消息挂一条时间线：

```ts
type TimelineNode =
  | { kind: 'model'; step; phase; model?; promptTokens?; completionTokens?; mode? }
  | { kind: 'tool';  id; step; phase; toolName; arguments?; result?; error?; allowed?; decision?; durationMs?; truncated? }
  | { kind: 'text';  content }
```

### 状态归并

- `tool_call`：按 `id` upsert——无则新建"进行中"，有则合并新字段。
- `model_call`：按 `step`+`kind:model` upsert，`invoked`→建节点，`responded`→补 token。
- 排序：`step` 升序，同 step 内按到达顺序。

### 渲染（执行时间线 + 分标签展开）

- 竖线时间线：工具节点圆点 `#6366f1`，模型节点圆点 `#f59e0b`，失败节点 `#ef4444`。
- **时间线本身默认展开**（一眼看到调了哪些工具/模型）；**每个节点内部默认收起**（一行摘要：动词 + 主要参数 + 状态/耗时）。
- 工具动词友好映射：`read_file`→"读取文件"、`execute_query`→"数据库查询"、`web_search`→"网页搜索"等；未映射回退显示原始 `tool_name`。
- 节点展开 → 分标签面板：**入参 / 结果 / 元数据**（元数据含耗时、token、`allowed`、`decision`、护栏）。`truncated:true` 时结果标签底部提示"已截断"。
- 进行中节点：圆点脉冲 + "执行中…"。

### 与 debug 面板关系

新时间线默认、始终可见（普通用户可见）；现有原始 `debug` 文本面板保留，作为开发者原始事件深挖入口。

## 错误处理与边界情况

1. **来源生命周期对齐**：channel 关闭 = 本轮结束（发 `done`）；bus 订阅在 channel 关闭后同步注销（`done`/`relayWg.Wait()`/`SetDefaultBus(prev)`）。用 `RequestID` 过滤 bus 事件，防并发会话串号。
2. **进行中节点收尾**：只收到 `started`/`invoked` 未见终态（断连/超时/ctx 取消）时，流结束后前端把残留"进行中"节点标记为"未完成/中断"，不留永久转圈。
3. **沿用错误抑制**：`suppressTerminalStreamError`（context canceled/deadline 且已有内容）不变；已渲染节点不因收尾错误被清空。
4. **护栏硬停**：现有 `DecomposeGuardrailRunError` banner 逻辑保留；对应工具节点用 `allowed=false`/`decision` 标记"护栏拦截"。
5. **大 payload**：8KB 截断在 portal 映射时执行，SSE 传输已是小体积。
6. **向后兼容**：不认识 `tool_call`/`model_call` 的老前端忽略这两类事件，`chunk`/`done` 行为不变。
7. **非流式路径**：`a.Run()` 调用方不受影响。

## 影响文件清单（实现参考）

- `framework/agent/react_agent.go` — 补 `ModelInvoked`/`ModelResponded` emit 的 token/model 字段。
- `portal/internal/service/chat.go` — `SendMessageStream` 改用 `RunEvents` + 始终订阅 bus + 映射截断。
- `portal/internal/service/chat_stream.go` — 新增 `ChatStreamEventToolCall`/`ChatStreamEventModelCall` 类型与 payload 结构体。
- `portal/internal/server/chat_sse.go` — 新增两个 SSE `case`。
- `web/src/api/client.ts` — SSE 解析新增 `onToolCall`/`onModelCall`。
- `web/src/pages/ChatPage.tsx` — 时间线数据模型、归并、渲染。
- `web/src/pages/ChatPage.css` — 时间线样式。
