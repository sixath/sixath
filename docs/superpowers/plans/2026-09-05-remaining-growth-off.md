# S34 Remaining Growth Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 拆掉剩余 Growth 平行 OS；skill_manage 写盘原语留在 skillops。

**Architecture:** 先把 lease/pin/patch/scan/tracker 迁入 `toolskill`，再删 `framework/growth` 与 Portal worker DI。`conf.Growth.llm` 留给 `/route`。

**Tech Stack:** Go（`framework/tool/skillops`、`framework/growth`、`portal` Wire）

**规格:** [`2026-09-05-remaining-growth-off-design.md`](../specs/2026-09-05-remaining-growth-off-design.md)

**分支:** 从 `feature/s33-append-learning-off` 切 `feature/s34-remaining-growth-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 迁入 skillops | `runtime_lease.go`、`pins.go`、`patch.go`、`applier.go`、`security.go`、`skills_index_tracker.go` 及对应 `_test.go` |
| 删除 | `framework/growth/` 其余文件 |
| 删除 Portal | worker/curator/growth usecase/repo/cron-ref/growthwake 及专测 |
| 改 | `skill_manager_tool.go`、`skill_tools.go`、`wire.go`、`wire_gen.go`、`main.go`、`data.go` AutoMigrate、ProviderSet |
| 锁定测试 | `framework/harness/growth_off_test.go`、`portal/internal/service/growth_off_test.go` |

禁止：regen proto；改 Channel auto_route；合 assembler。

---

### Task 1: 失败锁定测试

- [ ] `TestGrowthPackageRemoved`（`os.Stat("../growth")` 必须不存在）
- [ ] `TestGrowthWorkerGoRemoved`
- [ ] `TestMainGo_doesNotProvideGrowthWorker`
- [ ] 先跑应失败

---

### Task 2: 迁原语、删包、拆 Wire

- [ ] `git mv` 写盘原语到 skillops；`package toolskill`；skillops 去掉 `growth` import
- [ ] `git rm -r framework/growth`（迁走后剩余）
- [ ] 删 Portal growth/curator worker 栈；`wire_gen` 手改；AutoMigrate 去掉 growth 模型
- [ ] `cd framework && go test ./tool/skillops ./harness ./templates ./tool -count=1`
- [ ] `cd portal && go test ./internal/service ./internal/biz ./internal/data ./cmd/backend ./internal/runtime ./internal/conf ./internal/chat ./internal/server -count=1`
- [ ] **Commit** `fix(growth): drop unused growth OS after default path unwired`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
