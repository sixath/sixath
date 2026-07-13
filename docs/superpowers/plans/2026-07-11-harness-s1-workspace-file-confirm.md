# S1 余量：Workspace File 危险路径审批 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `write_file` / `patch` 增加与 terminal 同心态的危险路径两阶段确认；安全路径直写，零回归。

**Architecture:** `DangerPathPatterns` 命中 → PendingStore + `confirm_token`；Portal SSE `kind=workspace_file`。`RegisterWorkspaceFileTools` 无 store 时行为与今日一致。

**Tech Stack:** Go；复用 `TokenGenerator`；Portal `chat_stream` confirmation 提取。

> **Git：** 跳过 Commit（除非用户要求）。  
> **非目标：** process 后台栈、删除工具、全量写确认。

---

## 文件结构

| 文件 | 职责 |
|------|------|
| Create `framework/tool/file_pending.go` | Pending 模型 + InMemory store |
| Modify `framework/tool/file_tools.go` | Config、confirm 流、schema |
| Modify `framework/tool/file_tools_test.go` | danger propose/confirm / safe direct |
| Modify `portal/internal/chat/file_wiring.go` | 注入 store + TokenGen |
| Modify `portal/internal/service/chat_stream.go` | workspace_file confirmation |
| Modify gap S1 | workspace 审批已落地；process 仍 backlog |

---

### Task 1: Pending + write/patch

默认 DangerPathPatterns（路径相对 workspace，slash 规范化后匹配）：
- `(?i)(^|/)\.env($|\.)`
- `(?i)\.(pem|key|p12|pfx)$`
- `(?i)(^|/)id_rsa`
- `(?i)(^|/)credentials`
- `(?i)secrets?/`

行为：
1. 无 confirm_token + 危险路径 + store 配置 → pending（不写盘）
2. confirm_token → 执行 pending 的 path/content（或 patch 参数），删 pending
3. 安全路径 → 直写
4. 危险但无 store → `confirm_required_but_unconfigured`（不写）

- [x] TDD + `go test ./tool/ -run File -count=1`

### Task 2: Portal

- wiring 注入 InMemory + RandomTokenGenerator
- `workspaceFileConfirmationFromCall`：tool in {write_file,patch}，status=pending

- [x] TDD confirmation + wiring

### Task 3: Docs

- gap S1：workspace 已落地；process backlog

- [x] Docs

---

## 验收

- [x] `.env` write 需 confirm；普通 `.go` 直写
- [x] SSE kind=workspace_file
- [x] 无 store 注册路径与改造前一致（测试用无 store）
