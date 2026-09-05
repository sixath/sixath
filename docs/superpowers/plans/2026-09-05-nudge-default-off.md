# S18 Nudge Default Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Growth nudge 默认 Enabled=false；opt-in 仍走 env / SetNudgeConfig。

**Architecture:** 改 `DefaultNudgeConfig`。Portal env 包装继承该默认。测 pending 的用例显式打开 nudge。

**Tech Stack:** Go（`framework/growth`、portal `internal/biz`）

**规格:** [`2026-09-05-nudge-default-off-design.md`](../specs/2026-09-05-nudge-default-off-design.md)

**分支:** 从 `feature/s17-chat-growthuc-off` 切 `feature/s18-nudge-default-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 默认 | `framework/growth/config.go` + `config_test.go` |
| 注释 | `portal/internal/biz/growth.go`、`growth_nudge_env.go` |
| 测 pending | `portal/internal/biz/growth_test.go` 显式 Enabled=true |

禁止：删 OnToolSuccess；改 CuratorWorker；合 assembler。

---

### Task 1: 失败测试

- [ ] `TestDefaultNudgeConfig_enabledFalse`
- [ ] 先跑应失败

---

### Task 2: 改默认并修单测

- [ ] `DefaultNudgeConfig` Enabled=false；pending 用例显式打开
- [ ] `cd framework && go test ./growth -count=1`
- [ ] `cd portal && go test ./internal/biz ./internal/service -count=1`
- [ ] **Commit** `fix(growth): default nudge off the product path`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
