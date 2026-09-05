# S12 Unwire Growth ReAct Options Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 默认 Run 的 workspace hooks 走 `HarnessReActOptions`；删掉 growth 伪装装配与无调用者钩子。

**Architecture:** hooks 是 Harness KEEP。Growth 循环钩子无默认调用者则删。BackgroundReview 不动。

**Tech Stack:** Go（portal `internal/chat`、`internal/service`）

**规格:** [`2026-09-05-unwire-growth-react-opts-design.md`](../specs/2026-09-05-unwire-growth-react-opts-design.md)

**分支:** 从 `feature/s11-insights-shell-off` 切 `feature/s12-unwire-growth-react-opts`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 装配 | `portal/internal/chat/agent_builder.go`、`agent_builder_react_opts_test.go` |
| 默认路径 | `portal/internal/service/chat.go` |
| 删除 | `portal/internal/service/growth_chat.go`；`growth_session_hook_test.go` 里 notify 测试 |

禁止：删 `framework/growth`；改 GrowthWorker。

---

### Task 1: 失败测试

- [x] `TestHarnessReActOptions_LoadsWorkspaceHooks`
- [x] `TestChatGo_DoesNotCallGrowthReActOptions`
- [x] 先跑应失败

---

### Task 2: 接线与删除

- [x] hooks 并进 `HarnessReActOptions`；chat.go 去掉 `growthReActOptions`
- [x] 删 `growth_chat.go` 与无调用者测试
- [x] `cd portal && go test ./internal/chat ./internal/service -count=1`

- [x] **Commit** `fix(portal): move harness hooks out of growth ReAct options`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
