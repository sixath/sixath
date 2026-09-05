# S10 CLI Default Workspace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** CLI 空 workspace 落到 `{cwd}/.sath/workspace` 并保证目录存在。

**Architecture:** `workspace.EnsureCLIRoot` 纯函数；仅 CLI 入口调用。不改 `config.Load`。不拒跑。

**Tech Stack:** Go（`framework/workspace`、`framework/cli`）

**规格:** [`2026-09-05-cli-default-workspace-design.md`](../specs/2026-09-05-cli-default-workspace-design.md)

**分支:** 从 `feature/s9-chat-agent-workspace` 切 `feature/s10-cli-default-workspace`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| EnsureCLIRoot | `framework/workspace/cli_root.go`、`cli_root_test.go` |
| serve / demo | `framework/cli/serve.go`、`demo.go`、`workspace.go` |
| init 模板 | `framework/cli/init.go` |
| 注释 | `framework/config/config.go` Workspace 字段 |

禁止：改 Portal；Load/FromEnv 自动 MkdirAll；Insights；删别名。

---

### Task 1: 失败测试

- [x] `TestEnsureCLIRoot_EmptyCreatesDotSath`（`t.Chdir` 临时目录）
- [x] `TestEnsureCLIRoot_NonEmptyKeepsPath`
- [x] 跑测试确认失败

---

### Task 2: 接线

- [x] 实现 `EnsureCLIRoot`；`applyCLIWorkspace` 给 serve/demo
- [x] init `main.go` 模板接 `WithChatWorkspace(EnsureCLIRoot(...))`
- [x] `cd framework && go test ./workspace ./cli ./config ./templates ./harness -count=1`

- [x] **Commit** `fix(cli): default empty workspace to .sath/workspace`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
