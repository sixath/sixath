# S1：Terminal 危险命令审批 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `terminal` 工具补齐与 `execute_write` / `skill_manage` 同心态的危险命令两阶段确认（propose → `confirm_required` SSE → `confirm_token` 执行），对应 gap design S1 与 Hermes H-P0-F4。

**Architecture:** `DeniedPatterns` 仍硬拒绝；新增 `DangerPatterns` 命中则写入 `TerminalPendingStore` 并返回 `{status:pending,token,command,...}`；Portal 从 RunTrace 提取 `kind=terminal` 的 `confirm_required`。确认阶段只信任 pending 内的 command/workdir/timeout。

**Tech Stack:** Go；`framework/tool/terminal_*`；`portal/internal/service/chat_stream.go`；现有 `TokenGenerator` / SSE。

**Spec:** `docs/superpowers/specs/2026-07-11-harness-engineering-gap-design.md` S1；Hermes F4。

> **Git：** 无仓库则跳过 Commit。  
> **非目标：** process 后台工具（F2/F3）、pty、docker/ssh 后端、browser confirm、workspace `write_file` 审批（另议）。

---

## 现状锚点

| 代码 | 行为 |
|------|------|
| `terminal_tool.go` | denylist → `command_denied`；无审批 |
| `execute_write` / `skill_manage` | pending + `confirm_token` |
| `chat_stream.go` | `confirm_required` 仅 skill_manage / execute_write |

---

## 文件结构

| 文件 | 职责 |
|------|------|
| Create `framework/tool/terminal_pending.go` | Pending 模型、Store 接口、InMemory 实现 |
| Modify `framework/tool/terminal_tool.go` | DangerPatterns、confirm 流、schema `confirm_token` |
| Modify `framework/tool/terminal_tool_test.go` | 危险 propose / confirm / denylist 优先 |
| Modify `portal/internal/chat/terminal_wiring.go` | 注入 PendingStore + TokenGen |
| Modify `portal/internal/service/chat_stream.go` | `terminalConfirmationFromCall` |
| Modify `portal/internal/service/chat_stream_test.go` | kind=terminal 提取 |
| Modify gap design S1 行 | 已落地（terminal 审批；process 仍 backlog） |

---

### Task 1: PendingStore + DangerPatterns + terminal confirm

**Files:** `framework/tool/terminal_pending.go`、`terminal_tool.go`、`terminal_tool_test.go`

**行为锁死：**
1. denylist **先于** danger：`rm -rf /` → `command_denied`，不 pending
2. danger 命中且无 `confirm_token` → `status=pending`（`map[string]any`，含 `token`/`command`/`pattern`/`expires_in`）；**不**调 Runner
3. 带合法 `confirm_token` → 用 pending 的 command 执行，删 pending；非法/过期 → error map
4. 无 danger → 直接执行（与今日一致）
5. danger 命中但 `PendingStore==nil` → 不执行，返回 error（勿静默放行）

**默认 DangerPatterns（示例，可覆盖）：**
- `sudo`、`git push … --force`、`rm -rf`（非 denylist 根路径已由 denylist 吃掉）
- `curl|sh` / `wget|sh`、`dd if=`、`chmod -R 777`
- Windows：`Remove-Item … -Recurse`、`del /s`

- [x] **Step 1:** 写 FAIL 测：danger propose / confirm / denylist 优先
- [x] **Step 2:** 实现 Store + 改 RegisterTerminalTool
- [x] **Step 3:** PASS `go test ./tool/ -run Terminal -count=1`
- [x] **Step 4:** Commit（若有 git）`feat(tool): terminal danger command confirm flow`（无仓库则跳过）

---

### Task 2: Portal SSE `confirm_required` kind=terminal

**Files:** `chat_stream.go`、`chat_stream_test.go`

从 ToolCallRecord Result map 提取：`status=pending` + `token` + `command` →

```go
ChatConfirmationRequest{
  Kind: "terminal", Title: "Confirm terminal command",
  Description: "Review the shell command before it is executed.",
  Token, DSL: command, Severity: "danger", ExpiresIn,
}
```

- [x] **Step 1–4:** TDD + `go test ./internal/service/ -run Confirmation -count=1`

---

### Task 3: Portal wiring

**Files:** `portal/internal/chat/terminal_wiring.go`

```go
tool.RegisterTerminalTool(reg, &tool.TerminalConfig{
  Enabled: true,
  PendingStore: tool.NewInMemoryTerminalPendingStore(),
  TokenGen: tool.RandomTokenGenerator{},
})
```

- [x] **Step 1:** 接线；现有 terminal e2e 不回归
- [x] **Step 2:** Commit（无仓库则跳过）

---

### Task 4: Docs + gap status

- 更新 gap design Phase 2 / S1：terminal 审批已落地；process / workspace file 审批仍 backlog
- 可选一行说明于 hermes gap F4 或 portal 短注

- [x] **Step 1:** 文档
- [x] **Step 2:** 全量相关测试

---

## 验收

- [x] danger 命令不执行直至 confirm_token
- [x] denylist 仍硬拒绝
- [x] SSE 可发 `confirm_required` kind=terminal
- [x] 安全命令路径零行为变化
