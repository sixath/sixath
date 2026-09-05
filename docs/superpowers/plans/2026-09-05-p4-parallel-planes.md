# P4 Parallel Planes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 默认 Run 不再接线 Growth / MEA / Hub / HyperTool；SQL heal 与 query spill 退出默认自愈；Insights 退出默认导航；拆 MEA 后删除 `ShouldApplyEvidenceGate`；无调用者则删除 `plan_agent.go`。

**Architecture:** 包可以留着（规格 §6.3「不重写」），但 `SendMessage` / Stream / `Agent.Chat` 与 `BuildReActAgent` 默认路径不再调用它们。Workspace `hooks.yaml` 与 FailureCapture 仍挂在 ReAct（KEEP 免疫系统）。Hub HTTP 管理面可按需 `Init`，不在 `newChatService` 里默认拉起。

**Tech Stack:** Go（portal chat/service、framework tool/agent）、React（`AgentDetail.tsx`）

**规格:** [`2026-09-05-agent-model-workspace-harness-design.md`](../specs/2026-09-05-agent-model-workspace-harness-design.md) §6.2 / §6.3 / §11 P4

---

## File map

| 动作 | 路径 |
|------|------|
| 拆 Growth 主循环钩子 | `portal/internal/service/growth_chat.go`、`chat.go`、`cmd/backend/main.go`、`configs/*.yaml` |
| 拆 MEA 旁路 | `portal/internal/service/chat.go`、`mea_stream.go`+test；`portal/internal/chat/mea_*.go` |
| 拆 Hub 默认 Init | `service/chat.go` `newChatService`、`portal_agent_extra.go` |
| SQL heal 退出默认 | `framework/tool/data/execute_read.go`（不再 `queryWithSchemaHeal`） |
| query spill 退出默认 | `execute_read.go`、`framework/tool/es_log_tool.go`（不再 `MaybeSpill`） |
| Insights 退出导航 | `web/src/pages/AgentDetail.tsx`（保留 `App.tsx` 路由） |
| 删 `ShouldApplyEvidenceGate` | `agent_builder.go` + evalgolden / react_opts 测试 |
| 删无调用者 `plan_agent.go` | `framework/agent/plan_agent.go` + `_test.go` |
| HyperTool | Portal 不注册；`templates` 仅 `cfg.HyperTool.Enabled`（已是默认 false） |

禁止：`_neo4j_q/`；不删 `framework/growth` / `framework/mea` / `framework/memory/hub` 包；不删 Hub HTTP 处理文件。

---

### Task 1: Growth 退出默认 Run

- `growthReActOptions` 只保留 `LoadWorkspaceHarnessHooks` + FailureCapture；**不再** `WithReActToolSuccessHook`。
- `newChatService` 不再 `registerGrowthSessionHooks`。
- `chat.go` 不再 `notifyGrowthAssistantTurn` / `afterTurnBackgroundReview`。
- `newApp` 不再 `SetBackgroundReviewer`。
- `workerEnabled` 默认 **false**；`config.yaml` / `config.docker.yaml` 的 `growth.worker_enabled: false`。

### Task 2: 拆 MEA 旁路

- Stream 只走 `streamAgentEvents`。
- `git rm` portal `mea_*.go`、`mea_stream.go`+test。
- 删除 `ShouldApplyEvidenceGate` / `ShouldEnableEvidenceGate` / `rcaKeywordHit` 及测试。
- evalgolden 去掉 MEA 夹具。

### Task 3: Hub 不在 Chat 构造时 Init

- `newChatService` 去掉 `SetHubUnitWriter` + `InitLocalMemoryHub`。
- `SetPortalAgentExtra` 去掉无条件 `InitLocalMemoryHub()`。
- Hub HTTP（`memory_hub.go` / `hub_wire.go`）仍可按需 Init。

### Task 4: SQL heal / spill 退出默认自愈

- `execute_read`：`reader.Query` 失败即返回；不重写 SQL。
- `execute_read` / `es_log_query`：成功结果直接返回，不 `MaybeSpill`。
- 改 `TestExecuteRead_heals*`：期望 **不** 自动 heal。
- `HealReadSQL` / `MaybeSpill` 函数可留（器官细节 / 单测），默认路径不调用。

### Task 5: Insights 导航 + `plan_agent`

- `AgentDetail.tsx` 去掉 Insights 按钮；`App.tsx` 路由保留。
- `git rm` `framework/agent/plan_agent.go` `plan_agent_test.go`。

### Task 6: 回归

```
cd framework && go test ./agent ./tool ./model ./events ./mea ./tool/data -count=1
cd portal && go test ./internal/chat/... ./internal/service/... ./internal/conf/... ./internal/biz/... -count=1 -skip "TestNotifySessionMessageIndexed_WithDetachedCaller|TestSearchSessionsWithAgentFilterRequiresAgentUse"
```

验收：默认 Stream 无 `streamWithRulesMEA`；`BuildReActAgent` extra 无 growth success hook；无 `ShouldApplyEvidenceGate`；无 `PlanExecuteAgent`；Insights 不在 Agent 详情默认按钮。
