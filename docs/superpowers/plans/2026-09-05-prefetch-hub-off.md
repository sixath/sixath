# S28 Prefetch Hub Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `framework/memory` 包根不再 import `memory/hub`；prefetch loadout 过滤改为本地判断。

**Architecture:** 在 `store_prefetch_backend_test.go` 扫描包根 `*.go` 锁定无 hub import；再改 `prefetchUnitLoadoutEligible`。

**Tech Stack:** Go（`framework/memory`）

**规格:** [`2026-09-05-prefetch-hub-off-design.md`](../specs/2026-09-05-prefetch-hub-off-design.md)

**分支:** 从 `feature/s27-mea-off` 切 `feature/s28-prefetch-hub-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测试 | `framework/memory/store_prefetch_backend_test.go`（加锁定测试） |
| 改 | `framework/memory/store_prefetch_backend.go` |

禁止：删 `framework/memory/hub`、Portal `hub_*.go`；合 assembler。

---

### Task 1: 失败测试

- [ ] `TestMemoryPackageDoesNotImportHub`：包根 `*.go` 不含 `github.com/sixath/framework/memory/hub`
- [ ] 先跑应失败

---

### Task 2: 拆 import

- [ ] `prefetchUnitLoadoutEligible` 用本地 status / hub_status 字符串判断，逻辑与 `LoadoutEligible(MapUnitToAssetStatus(...))` 一致
- [ ] `cd framework && go test ./memory -count=1`
- [ ] `cd portal && go test ./internal/chat ./internal/service -count=1`（skip `TestNotifySessionMessageIndexed_WithDetachedCaller`）
- [ ] **Commit** `fix(memory): drop hub import from store prefetch backend`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
