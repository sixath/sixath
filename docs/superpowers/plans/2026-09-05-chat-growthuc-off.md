# S17 ChatService Drop growthUC Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ChatService 构造不再注入未使用的 GrowthUsecase。

**Architecture:** 字段与参数删掉。wire 调用同步。GrowthUsecase 仍供给 opt-in worker/curator。

**Tech Stack:** Go（portal `internal/service`、`cmd/backend/wire_gen.go`）

**规格:** [`2026-09-05-chat-growthuc-off-design.md`](../specs/2026-09-05-chat-growthuc-off-design.md)

**分支:** 从 `feature/s16-background-review-off` 切 `feature/s17-chat-growthuc-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 失败测试 | `portal/internal/service/chat_growthuc_off_test.go` |
| ChatService | `portal/internal/service/chat.go` |
| 调用方 | `chat_workspace_run_test.go`、`browser_session_hooks_test.go`、`growth_session_hook_test.go` |
| wire | `portal/cmd/backend/wire_gen.go`（手改调用，Keep ProvideGrowthUsecase） |

禁止：删 `framework/growth`；改 CuratorWorker；合 assembler。

---

### Task 1: 失败测试

- [x] `TestChatGo_DoesNotHoldGrowthUC`
- [x] 先跑应失败

---

### Task 2: 拆参数

- [x] 删字段与构造参数；改测试调用；改 `wire_gen.go`
- [x] `cd portal && go test ./internal/service ./cmd/backend -count=1`
- [x] **Commit** `fix(portal): drop unused growthUC from ChatService`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
