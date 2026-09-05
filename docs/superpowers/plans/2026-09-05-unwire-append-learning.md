# S23 Unwire append_learning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 默认 Chat / 快捷 Chat 不再把 append_learning 装进 registry。

**Architecture:** 删三处 `RegisterLearningTools` 调用。保留函数。源码锁定测试。

**Tech Stack:** Go（portal `internal/service`）

**规格:** [`2026-09-05-unwire-append-learning-design.md`](../specs/2026-09-05-unwire-append-learning-design.md)

**分支:** 从 `feature/s22-skill-autoroute-off` 切 `feature/s23-unwire-append-learning`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测试 | `portal/internal/service/learning_tools_off_test.go` |
| 装配 | `portal/internal/service/chat.go`、`agent.go` |

禁止：删 `RegisterLearningTools`；合 assembler。

---

### Task 1: 失败测试

- [ ] `TestChatGo_DoesNotRegisterLearningTools` / `TestAgentGo_DoesNotRegisterLearningTools`
- [ ] 先跑应失败

---

### Task 2: 拆装配

- [ ] 去掉三处调用
- [ ] `cd portal && go test ./internal/service ./internal/chat -count=1`
- [ ] **Commit** `fix(portal): unwire append_learning from default chat`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
