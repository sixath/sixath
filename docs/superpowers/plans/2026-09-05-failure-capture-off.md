# S32 Failure Capture Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除无生产调用者的 `FailureCaptureHook`；把 `WithRequestMetadata` 迁出以免 ReAct 断线。

**Architecture:** `os.Stat("failure_capture_hook.go")` 锁定文件不存在；helpers 放到 `request_metadata.go`。

**Tech Stack:** Go（`framework/harness`）

**规格:** [`2026-09-05-failure-capture-off-design.md`](../specs/2026-09-05-failure-capture-off-design.md)

**分支:** 从 `feature/s31-growth-shell-leftover-off` 切 `feature/s32-failure-capture-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测试 | `framework/harness/failure_capture_off_test.go` |
| 新增 | `framework/harness/request_metadata.go` |
| 删除 | `framework/harness/failure_capture_hook.go`、`failure_capture_hook_test.go` |

禁止：删 growth；合 assembler。

---

### Task 1: 失败测试

- [ ] `TestFailureCaptureHookFileRemoved`：`failure_capture_hook.go` 必须不存在
- [ ] 先跑应失败

---

### Task 2: 迁 helpers 并删 Hook

- [ ] `request_metadata.go` 承接 `WithRequestMetadata` / `RequestMetadataFromContext`
- [ ] `git rm` hook 文件
- [ ] `cd framework && go test ./harness ./tool ./templates -count=1`
- [ ] **Commit** `fix(harness): drop unused FailureCaptureHook after default path unwired`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
