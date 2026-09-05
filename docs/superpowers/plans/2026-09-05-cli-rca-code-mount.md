# S7 CLI RCA Code Mount Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** CLI templates 的 `rca_grep` 只使用 `workspace/code`；`config.Workspace` 交给 ReAct。

**Architecture:** 与 Portal S4 相同：`ResolveCodeMount` 有值才 `RegisterRCACodeTools`。`rca.repos.roots` 忽略。jaeger/ES 不动。

**Tech Stack:** Go（`framework/config`、`framework/templates`）

**规格:** [`2026-09-05-cli-rca-code-mount-design.md`](../specs/2026-09-05-cli-rca-code-mount-design.md)

**分支:** 从 `feature/s6-whole-repo-update-reject` 切 `feature/s7-cli-rca-code-mount`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| `Workspace` 字段 | `framework/config/config.go`、`FromEnv` / `ApplyEnvOverrides`、`config_test.go` |
| RCA 注册 | `framework/templates/rca_wiring.go`、`rca_wiring_test.go` |
| ReAct workspace | `framework/templates/skills_handler.go` |

禁止：改 Portal MergeRCARoots；改 jaeger/ES；扫示例 yaml。

---

### Task 1: Config.Workspace

- [x] YAML `workspace`；`AGENT_WORKSPACE` 写入 FromEnv 与 ApplyEnvOverrides。
- [x] `TestRCAConfig_YAML` 可顺带断言 workspace 字段；`TestApplyEnvOverrides` 覆盖 AGENT_WORKSPACE。

---

### Task 2: registerRCATools + skills handler

- [x] 无 mount、仅 roots → 不注册 rca_grep。有 `workspace/code` → 注册，忽略 roots。
- [x] `NewSkillsAwareChatHandlerFromConfig`：非空 workspace → `WithReActWorkspace`。
- [x] `cd framework && go test ./config ./templates ./workspace -count=1`

- [x] **Commit** `fix(templates): register rca_grep only from workspace/code`

---

### Task 3: 回归

- [x] 不要 merge/push，除非用户明确要求。
