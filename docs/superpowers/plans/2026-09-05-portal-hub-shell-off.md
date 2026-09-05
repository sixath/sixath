# S29 Portal Hub Shell Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除 Portal Hub 管理面 leftover（handler / chat hub_* / wire / mysql binding store）并拆 Web Hub 字段。

**Architecture:** 用 `os.Stat` 锁定关键文件不存在；`http.go` 不含 hub 路径；Web 表单不含 hub-governance testid。然后 `git rm` 并列文件并改 Web。

**Tech Stack:** Go、React（AgentForm / AgentDetail / client.ts）

**规格:** [`2026-09-05-portal-hub-shell-off-design.md`](../specs/2026-09-05-portal-hub-shell-off-design.md)

**分支:** 从 `feature/s28-prefetch-hub-off` 切 `feature/s29-portal-hub-shell-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测试 | `portal/internal/chat/hub_off_test.go`、`portal/internal/server/http_hub_off_test.go`、`portal/internal/service/hub_off_test.go` |
| 删除 | `portal/internal/chat/hub_*.go`、`portal/internal/server/memory_hub*.go`、`portal/internal/service/hub_*.go`、`portal/internal/data/binding_store_mysql.go`（及测试） |
| 改 | `web/src/pages/AgentForm.tsx`、`AgentDetail.tsx`、`web/src/api/client.ts` |

禁止：删 `framework/memory/hub`；合 assembler。

---

### Task 1: 失败测试

- [ ] `TestHubBootstrapFileRemoved`：`hub_bootstrap.go` 必须不存在
- [ ] `TestMemoryHubServerFileRemoved`：`memory_hub.go` 必须不存在
- [ ] `TestHubWireFileRemoved`：`hub_wire.go` 必须不存在
- [ ] `TestHTTP_OmitsHubRoutes`：`http.go` 不含 `/hub/` 与 `memory-hub`
- [ ] `TestWebAgentFormOmitsHubGovernance`：`AgentForm.tsx` 不含 `hub-governance`
- [ ] 先跑应失败（除 HTTP 路由扫描：现网已不注册，可先绿）

---

### Task 2: 删文件并拆 Web

- [ ] `git rm` 上表删除列
- [ ] 去掉表单/详情 Hub 字段与 `client.ts` hub_* 序列化
- [ ] `cd portal && go test ./internal/chat ./internal/service ./internal/server ./internal/data -count=1`（skip `TestNotifySessionMessageIndexed_WithDetachedCaller`）
- [ ] **Commit** `fix(portal): drop unused hub management surface leftover`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
