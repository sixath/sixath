# S24 Remaining Default-Path Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 默认 Run 不再凭据回拉、不再拼任务锁、不再每轮 extract/graph、不再装 prefetch、不再建 toolFamily 索引。

**Architecture:** 源码锁定测试先行。Harness 与 Portal 分文件改。保留器官实现与包。

**Tech Stack:** Go（`framework/harness`、portal chat/service）

**规格:** [`2026-09-05-remaining-default-path-off-design.md`](../specs/2026-09-05-remaining-default-path-off-design.md)

**分支:** 从 `feature/s23-unwire-append-learning` 切 `feature/s24-remaining-default-path-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测试 | `framework/harness/default_path_off_test.go`、`portal/internal/service/default_path_off_test.go`、`portal/internal/chat/default_path_off_test.go` |
| Harness | `framework/harness/react_agent.go`、删 `evidence_tools.go`、改 credential/task-lock 测试 |
| Portal | `chat.go`、`agent_builder.go`、`mcp_expand.go` |

禁止：合 assembler；删 growth/mea/hub 包。

---

### Task 1: 失败测试

- [x] 源码锁定：react_agent / chat.go / agent_builder 不得含上述字符串
- [x] 先跑应失败

---

### Task 2: 实现

- [x] 拆循环与装配
- [x] `cd framework && go test ./harness ./tool -count=1`
- [x] `cd portal && go test ./internal/chat ./internal/service -count=1`
- [x] **Commit** `fix: unwire remaining default-path leftover gates`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
