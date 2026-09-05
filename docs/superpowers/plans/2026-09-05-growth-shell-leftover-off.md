# S31 Growth Shell Leftover Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 拆掉默认外壳上的 Growth metrics 与 FailureCapture 装配；删零调用 `growth_metadata.go`。

**Architecture:** 源码扫描锁定 http.go / agent_builder.go / main.go；`os.Stat` 锁定已删文件。

**Tech Stack:** Go（portal server / chat / cmd/backend）

**规格:** [`2026-09-05-growth-shell-leftover-off-design.md`](../specs/2026-09-05-growth-shell-leftover-off-design.md)

**分支:** 从 `feature/s30-memory-hub-off` 切 `feature/s31-growth-shell-leftover-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测试 | `portal/internal/server/http_growth_off_test.go`、`portal/internal/chat/growth_shell_off_test.go` |
| 删 | `portal/internal/server/growth_metrics.go`、`portal/internal/chat/growth_metadata.go` |
| 改 | `portal/internal/server/http.go`、`portal/internal/chat/agent_builder.go`、`portal/internal/chat/skill_manage_confirm.go`、`portal/cmd/backend/main.go` |

禁止：删 `framework/growth`；改 skillops；合 assembler。

---

### Task 1: 失败测试

- [ ] `TestHTTP_OmitsGrowthMetricsRoute`
- [ ] `TestGrowthMetricsFileRemoved`
- [ ] `TestHarnessGo_doesNotWireFailureCapture`
- [ ] `TestGrowthMetadataFileRemoved`
- [ ] 先跑应失败

---

### Task 2: 拆装配

- [ ] 删 metrics 路由与文件；Harness 不再注入 FailureCapture；main 改读 skill-manage confirm env
- [ ] `cd portal && go test ./internal/chat ./internal/server ./cmd/backend -count=1`（skip SQLITE_BUSY）
- [ ] **Commit** `fix(portal): drop growth metrics shell and failure-capture default wiring`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
