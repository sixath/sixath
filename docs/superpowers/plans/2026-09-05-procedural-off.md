# S40 Procedural Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除 procedural five-gate 与 prefetch/yaml 死键；保留 `KindProcedural` 历史过滤与 FailureSignal。

**Architecture:** 先锁源文件/类型不存在，再删文件并修 prefetch / config / 测试编译。

**Tech Stack:** Go（`framework/memory`、`framework/config`、portal chat/data）

**规格:** [`2026-09-05-procedural-off-design.md`](../specs/2026-09-05-procedural-off-design.md)

**分支:** 从 `feature/portal-assembler` 切 `feature/s40-procedural-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测 | `framework/memory/procedural_off_test.go`、`framework/config/procedural_cfg_off_test.go` |
| 删 | `framework/memory/procedural_commit.go`、`procedural_commit_test.go`、`procedural_binding.go`、`procedural_binding_test.go`、`procedural_catalog.go`、`procedural_catalog_test.go`、`portal/internal/data/procedural_mysql_e2e_test.go` |
| 改 | `store_prefetch_backend.go`、`store.go`、`kind.go`、`config/tool_guardrails.go`、portal prefetch 锁定测试、`agent_extra.yaml` 注释 |

禁止：改 Channel；改 MaybeSpill；DROP 表；合 assembler（除非用户要求）。

---

### Task 1: 失败锁定测试

- [ ] `TestProceduralCommitGoRemoved` 等
- [ ] `TestConfigGo_omitsMemoryProceduralRepair`
- [ ] 先跑必须红

---

### Task 2: 删 five-gate 并修好调用方

- [ ] 删 procedural_* 文件与 e2e
- [ ] prefetch 去掉注入字段与匹配循环
- [ ] config 去掉 `MemoryProceduralRepair`
- [ ] 保留 `KindProcedural` / Remember 拒写 / FailureSignal
- [ ] **Commit** `fix(memory): drop unused procedural five-gate after portal unwired`

---

### Task 3: 回归

- [ ] `cd framework && go test ./memory ./config ./harness -count=1`
- [ ] `cd portal && go test ./internal/chat ./internal/data -count=1`（skip SQLITE_BUSY）
- [ ] 不要 merge/push，除非用户明确要求。
