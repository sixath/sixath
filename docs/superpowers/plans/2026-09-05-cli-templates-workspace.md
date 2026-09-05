# S8 CLI Templates Workspace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** dataquery 与 MCP 的 ReAct 装配接上非空 `workspace`，与 skills handler 同一条故事。

**Architecture:** 抽出 `appendWorkspaceOpt`；`DataQueryConfig.Workspace` 从根 `config.Config` 拷入；`templates.Config.Workspace` 给 MCP。空字符串不传选项。不改 ChatAgent / Portal / RCA。

**Tech Stack:** Go（`framework/templates`）

**规格:** [`2026-09-05-cli-templates-workspace-design.md`](../specs/2026-09-05-cli-templates-workspace-design.md)

**分支:** 从 `feature/s7-cli-rca-code-mount` 切 `feature/s8-cli-templates-workspace`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| helper | `framework/templates/react_opts.go` |
| dataquery | `framework/templates/dataquery.go`、`dataquery_test.go` |
| MCP | `framework/templates/mcp.go`、`mcp_test.go` |
| skills | `framework/templates/skills_handler.go`（改用 helper） |

禁止：改 Portal；改 ChatAgent；dataquery 注册 rca；扫示例 yaml。

---

### Task 1: 失败测试（MEMORY.md 信号）

- [ ] `TestNewDataQueryHandler_InjectsWorkspaceMemoryMD`：TempDir + `MEMORY.md`，`DataQueryConfig.Workspace` 指向该根，断言 `fakeToolModel.lastMessages` 含 `## MEMORY.md` 与正文。
- [ ] `TestNewMCPAgentHandler_InjectsWorkspaceMemoryMD`：同上；空白 Workspace 的对照不出现该正文。
- [ ] 跑测试确认失败（尚未接线）。

---

### Task 2: 接线

- [ ] `appendWorkspaceOpt`；dataquery / MCP / skills 共用。
- [ ] `DataQueryConfig.Workspace`；`NewDataQueryHandlerFromConfig` 拷 `cfg.Workspace`。
- [ ] `templates.Config.Workspace`。
- [ ] `cd framework && go test ./templates ./config -count=1`

- [ ] **Commit** `fix(templates): wire workspace into dataquery and mcp ReAct`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
