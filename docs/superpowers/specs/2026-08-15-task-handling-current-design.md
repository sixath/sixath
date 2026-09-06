# 收到任务后怎么处理（现状说明）

> 状态：文档-only（描述现行行为，不引入行为变更）  
> 日期：2026-08-15  
> 范围：Portal Web 聊天入口 → MEA 外环 / 纯 ReAct → Framework ReAct 内环 → 工具与 SSE  
> 读者：新人上手 + 排障  
> 相关：[MEA M0](./2026-08-12-mea-minimal-subset-design.md)、[MEA M0.5 Chat 接线](./2026-08-12-mea-m05-chat-wire-design.md)、[MEA M1 LLM Auditor](./2026-08-13-mea-m1-llm-auditor-design.md)、[portal/docs/mea-m0.md](../../../portal/docs/mea-m0.md)

## 1. 目标与非目标

### 目标

用一篇端到端说明，回答：「用户发一条消息（任务）之后，系统按什么顺序处理？」覆盖：

- 入口分流（MEA 编排 vs 纯 ReAct）
- Portal 关键步骤与文件
- Framework 两层循环（MEA 外环 + ReAct 内环）
- SSE 事件对照与排障决策树

### 非目标

- 不改变任何运行时行为
- 不规定产品应如何改入口策略（例如「勾了 MEA 就自动进」）
- 不展开企微/Gateway 协议细节（二者最终仍进入同一套 Chat 流式路径时，适用本文后半）

## 2. 端到端总览

```
用户消息
  │
  ├─ 取 session / agentMeta → BuildModel
  ├─ 解析 ```mea-checks``` / ```mea-acceptance```
  │     （仅 ok=true 且非空才算「有验收块」；非法 JSON / 空数组不算）
  ├─ 装 tools + skills + MCP + ReAct Agent
  ├─ 落库 user message（ok 时已剥 fence；解析失败时 fence 可能仍在正文）
  │
  ▼
useMEA = (成功解析出非空验收)
       AND MEAEnabledForAgent(...)
       AND workspace 非空
  │
  ├─ 是 → streamWithRulesMEA
  │         Manager → Execute(一次完整 ReAct) → Audit → …
  │         SSE: chunk / tool_call / model_call / mea
  │
  └─ 否 → streamAgentEvents（纯 ReAct 一次 episode）
            SSE: chunk / tool_call / model_call
```

说明：上图顺序与 `SendMessageStream` 实现一致（`BuildModel` 在首次解析之前；Registry/ReAct 在解析之后、`CreateMessage` 之前）；分流决策不依赖「先落库」。

### 关键结论

| 结论 | 含义 |
|------|------|
| 入口互斥 | 一次 turn 只走 MEA 外层 **或** 纯 ReAct 外层 |
| 内部不互斥 | MEA 的 Execute 仍是现有 ReAct（`RunEvents`） |
| 验收必需 | 须 **成功解析** 出非空 `mea-checks` 或 `mea-acceptance`；仅有 fence 外观不够。无有效验收 → 永不进 MEA（即使 UI 勾了） |
| MEA 不是工具 | 没有名为 `Manage-Execute-Audit` 的可调用 tool；MEA 是编排 |

## 3. Portal 逐步走读

主入口：`ChatService.SendMessageStream`（`portal/internal/service/chat.go`）。

| 步骤 | 做什么 | 关键位置 |
|------|--------|----------|
| 1 | HTTP/SSE 进入流式发送 | `SendMessageStream` |
| 2 | 取 session、agentMeta（含 `Workspace`、`RuntimeTools.MEAEnabled`）；`BuildModel` | chat UC / agent UC / `BuildModel` |
| 3 | 解析验收块：`ok=true` 才剥离并采用；非法 JSON / 空数组 / 无有效 `type` → `ok=false`，不进 MEA | `portal/internal/chat/mea_parse.go` |
| 4 | 本轮工具面 + Registry + MCP | `PrepareTurnToolSurface` 等 |
| 5 | 装 ReAct Agent；本轮私有 `events.Bus` → SSE relay | `BuildReActAgent` / bus 订阅 |
| 6 | 落库 user message（仅解析成功时正文无 fence） | `CreateMessage` |
| 7 | `useMEA` 三条件分流 | `chat.go` 中 `useMEA := ...` |
| 8 | 写出 SSE | `portal/internal/chatsse/sse.go` |

### MEA 开关（OR）

实现：`portal/internal/chat/mea_flag.go` → `MEAEnabledForAgent`。

任一为真即可：

1. Agent UI：`runtime_tools.mea_enabled`
2. 全局环境：`SATH_MEA=1|true|yes|on`
3. 试点列表：`SATH_MEA_PILOT_AGENTS` 含该 agent id

### 进入 MEA 后的 Portal 薄层

- `streamWithRulesMEA`（`portal/internal/service/mea_stream.go`）
  - 发 `mea` phase=`started`
  - 构造 `ExecutorFunc`：每轮调用同一套 `streamAgentEvents`（ReAct），再发 `mea` phase=`round`
  - 调用 `chat.RunRulesMEA`（`portal/internal/chat/mea_run.go`）
  - 结束后发 `mea` phase=`finished`（含 reason / pending / completed）
- `RunRulesMEA` 装配 `BootstrapManager` + Rules / Cascade(+LLM) Auditor + file Store，再跑 framework `mea.Orchestrator`

Executor 会把当前 `Contract.Goal` 与 acceptance 摘要写回本轮 user 消息（`messagesForMEAContract`），再交给 ReAct。

`mea-checks` 与 `mea-acceptance` 可同条消息并存；有结构化 checks 时 Cascade Auditor 优先走 Rules（文字 acceptance 主要用于无 checks 时的 LLM 路径，见 M1）。

## 4. Framework：MEA 外环 + ReAct 内环

### 4.1 MEA 外环

文件：`framework/mea/orchestrator.go`（默认最多约 25 round）。

```
loop:
  Manager.Decide → Contract
  Executor.Execute     ← Portal 注入：一整次 ReAct RunEvents
  Auditor.Audit        ← RulesAuditor 和/或 CascadeAuditor(+LLMAuditor)
  ApplyAudit → Store.Save
  until Ask | Done | Blocked | max_rounds
```

纯文字 acceptance（无结构化 checks）时，M1 路径可走 LLM Auditor（见 M1 规格）；有结构化 checks 时优先 Rules。

### 4.2 ReAct 内环

文件：`framework/agent/react_agent.go`。

```
for step < MaxSteps:
  准备/压缩上下文 → Model
  无 tool_calls → 文本结束（可能经 evidence gate）
  有 tool_calls → executeToolStep
       Registry / MCP / skills 等
       emit tool_started | completed | failed 等
  护栏可 halt
触顶 → max steps 错误 / force final summary
```

| 路径 | 循环次数 |
|------|----------|
| 纯 ReAct | 一次 episode（内环 MaxSteps） |
| MEA | 外环每 round 再跑 **一整次** 内环；SSE 上会看到多轮 tool_call，并穿插 `event: mea` |

## 5. SSE 事件对照

写出层：`portal/internal/chatsse/sse.go`（`WriteStream`）。

| SSE `event` | 含义 | 备注 |
|-------------|------|------|
| `chunk` | 助手正文增量 | 聚合后落库；timeline 不把 debug/tool 混进正文 |
| `tool_call` | 工具生命周期 | 来自 agent StreamEvent → Portal 映射 |
| `model_call` | 模型调用时间线 | 本轮私有 bus 转发 |
| `mea` | MEA 编排进度 | 仅 MEA 路径；payload：`started` / `round` / `finished` |
| `input_required` / `confirm_required` / `confirm_result` | HITL | 可中断「看起来卡住」的观感 |
| `debug` | 调试串 | DebugRun 时 |
| `error` | 失败 | 部分 deadline 在已有内容时可被抑制 |
| `done` | 流正常结束 | |

MEA payload 形状见 `service.MEAStreamPayload`（`portal/internal/service/chat_stream.go`）：`Phase`、`Round`、`Pending`、`Completed`、`Goal`、`Reason` 等。

## 6. 排障附录

### 6.1 为什么没进 MEA？

```
流里是否出现 event: mea ?
  ├─ 有 → 已进 MEA；看 phase / reason / pending|completed
  └─ 无 → 同时检查（任一否都不进）:
        ① 验收是否「解析成功且非空」？
           - 看得见 ```mea-checks``` / ```mea-acceptance``` 不够
           - JSON 非法、空数组、checks 无有效 type、acceptance 全空字符串
             → ParseMEA* 返回 ok=false → 不进 MEA（设计如此）
           - 调用方仅在 ok=true 时用 clean 替换正文；失败时 fence 可能仍留在落库消息里
        ② MEAEnabledForAgent？（UI / SATH_MEA / PILOT）
        ③ agent.workspace 是否非空？
        → 否则走纯 ReAct（设计如此，不是故障）
```

### 6.2 其它常见卡点

| 现象 | 优先看 |
|------|--------|
| 有 `tool_call` 无最终 `chunk` | 护栏 halt、MaxSteps、HITL confirm/input |
| `mea` finished 但验收未过 | Auditor（Rules vs LLM cascade）、workspace 相对路径、checks 是否可在 WorkDir 机检 |
| 企微长时间「处理中」 | 工具超时/内网连通；与入口分流无关 |
| 勾了 MEA 仍像普通聊天 | 缺有效验收（含解析失败）、开关未开、或 workspace 空 → 见 6.1 |

### 6.3 验收块示例（便于对照）

````text
把 out.txt 写好并带上 hello。

```mea-checks
[
  {"type":"path_exists","path":"out.txt"},
  {"type":"file_contains","path":"out.txt","pattern":"hello"}
]
```
````

或使用文字验收（走 LLM Auditor 路径，需 AuditorModel 可用）：

````text
完成部署并确认服务健康。

```mea-acceptance
["health endpoint returns 200", "version file updated"]
```
````

## 7. 维护约定

- 本文描述 **代码现状**；若 `useMEA` 条件或 SSE 形状变更，应同步改本文。
- 行为变更仍以 MEA 各阶段 design/plan 为准；本文不替代那些规格。
