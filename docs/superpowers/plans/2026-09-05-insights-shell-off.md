# S11 Insights Shell Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Insights 退出 Web 与 Portal HTTP 外壳；Rewind / turn-trace 留下。

**Architecture:** 删聚合页与只读 API。`rewind_insights.go` 去掉 InsightsHandler。锁测试断言 `http.go` 不再注册 `/insights`。

**Tech Stack:** Go（portal）、React（web）

**规格:** [`2026-09-05-insights-shell-off-design.md`](../specs/2026-09-05-insights-shell-off-design.md)

**分支:** 从 `feature/s10-cli-default-workspace` 切 `feature/s11-insights-shell-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 锁测试 | `portal/internal/server/http_insights_off_test.go` |
| HTTP | `portal/internal/server/http.go`、`rewind_insights.go` |
| 聚合 | 删 `portal/internal/service/insights.go`、`insights_test.go` |
| Web | `web/src/App.tsx`、删 `AgentInsightsPage.tsx`、`web/src/api/client.ts` |

禁止：删 Rewind；删 turn-trace；改 Growth 包。

---

### Task 1: 失败测试

- [ ] `TestHTTP_OmitsInsightsRoute`：读 `http.go`，断言不含 `/insights`。先跑应失败。

---

### Task 2: 拆除

- [ ] 去路由、Handler、service、页面、client。
- [ ] Rewind 仍注册。
- [ ] `cd portal && go test ./internal/server ./internal/service -count=1`

- [ ] **Commit** `fix(portal): remove Insights from product shell`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
