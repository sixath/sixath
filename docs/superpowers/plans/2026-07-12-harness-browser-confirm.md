# S2 余量：Browser 危险动作确认 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** 为 `browser_navigate` / `browser_click` / `browser_type` 增加与 terminal/workspace_file 同心态的 `confirm_token` 两阶段确认；`browser_snapshot` 只读不确认。

**Architecture:** `BrowserPendingStore` + Portal 注入；无 store 时零回归直执行。SSE `kind=browser`。

> **非目标：** 下载策略、全栈 browser_*、按域名 allowlist（可后续加）。

---

## 行为

| 工具 | 有 PendingStore | 无 store |
|------|-----------------|----------|
| navigate / click / type | propose → confirm | 直执行 |
| snapshot | 直执行 | 直执行 |

---

## 文件

| 文件 | 职责 |
|------|------|
| Create `framework/tool/browser_pending.go` | Pending + InMemory |
| Modify `framework/tool/browser_tools.go` | Config + confirm 流 |
| Modify `browser_tools_test.go` | propose/confirm / snapshot 免确认 |
| Modify `portal/.../browser_wiring.go` | 注入 store |
| Modify `chat_stream.go` | `browserConfirmationFromCall` |
| Modify gap S2 | confirm 已落地 |

---

## 验收

- [x] navigate 有 store 时不真正 Navigate 直至 confirm
- [x] snapshot 从不 pending
- [x] SSE kind=browser
- [x] 无 store 直执行（测试 Fake 路径）
