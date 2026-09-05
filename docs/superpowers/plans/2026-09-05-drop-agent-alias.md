# S13 Drop Agent Alias Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除无仓内调用者的 `framework/agent` 一季别名，骨架只剩 `harness`。

**Architecture:** 别名只做类型/构造转发。删包不改循环。历史文档路径字符串保留。

**Tech Stack:** Go（`framework/harness`、删除 `framework/agent`）

**规格:** [`2026-09-05-drop-agent-alias-design.md`](../specs/2026-09-05-drop-agent-alias-design.md)

**分支:** 从 `feature/s12-unwire-growth-react-opts` 切 `feature/s13-drop-agent-alias`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 失败测试 | `framework/harness/agent_alias_off_test.go` |
| 删除 | `framework/agent/alias.go`、`framework/agent/alias_test.go`（整个目录） |

禁止：删 ChatStream；改 GrowthWorker；改 Portal `agent` 标识符；合 assembler。

---

### Task 1: 失败测试

- [ ] `TestAgentAliasPackageRemoved`：`../agent` 目录必须不存在
- [ ] 先跑应失败

---

### Task 2: 删包

- [ ] `git rm -r framework/agent`
- [ ] `cd framework && go test ./harness ./workspace ./context ./model ./templates -count=1`
- [ ] **Commit** `fix(framework): drop one-season agent alias package`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
