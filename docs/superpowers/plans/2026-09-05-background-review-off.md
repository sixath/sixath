# S16 Background Review Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** C3 退出默认 Chat 装配；GrowthWorker 只在 worker_enabled 时构造。

**Architecture:** 删 ChatService after-turn 死钩子。Worker poll / `runForkReviewAgent` 留下。`framework/growth` 不删。

**Tech Stack:** Go（portal `internal/biz`、`internal/service`、`cmd/backend`）

**规格:** [`2026-09-05-background-review-off-design.md`](../specs/2026-09-05-background-review-off-design.md)

**分支:** 从 `feature/s15-procedural-portal-off` 切 `feature/s16-background-review-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。不要 `wire` regen。

---

## File map

| 动作 | 路径 |
|------|------|
| 默认 C3 off | `portal/internal/biz/growth_nudge_env.go` + 测试 |
| 失败测试 | `portal/internal/service/background_review_off_test.go` |
| 拆 Chat C3 | `chat.go` 字段；`background_review.go` 删 after-turn / SpawnBackgroundReview |
| worker 构造 | `portal/cmd/backend/main.go` `provideGrowthWorker` |
| 删测试 | `TestAfterTurnBackgroundReview_*`；truncate/synthetic 若无调用者一并删 |

禁止：删 `framework/growth`；改 CuratorWorker；regen wire。

---

### Task 1: 失败测试

- [ ] `TestBackgroundReviewEnabledFromEnv_defaultFalse`
- [ ] `TestBackgroundReviewGo_NoAfterTurnHook`
- [ ] `TestProvideGrowthWorkerSource_NilWhenDisabled`
- [ ] 先跑应失败

---

### Task 2: 拆接线

- [ ] env 默认 false；`!workerEnabled` 不构造 worker；删 ChatService C3 钩子
- [ ] `cd portal && go test ./internal/biz ./internal/service -count=1`
- [ ] **Commit** `fix(portal): keep background review off the default chat path`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
