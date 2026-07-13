# S1 余量：Terminal Process 后台栈（F2/F3 最小集）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 Hermes H-P0-F2/F3 最小集：`terminal(background=true)` 返回可管理的 `session_id`；`process` 支持 list/poll/log/wait/kill；poll 增量日志；kill 后状态一致。

**Architecture:** 进程内 `ProcessRegistry`（按 chat `session_id` 索引）；后台用独立 context（不随 HTTP 取消）；输出环形/截断缓冲；Portal 与 terminal 共享同一 registry。

**Tech Stack:** Go `os/exec`；Windows `cmd /C` 与 Unix `sh -c` 同现有 foreground。

> **非目标 / waiver：** pty；watch_patterns 熔断；崩溃 JSON 检查点；notify_on_complete 唤醒 Agent（仅标记 + poll 可见）。stdin write/submit/close **已补齐**。

---

## 文件结构

| 文件 | 职责 |
|------|------|
| Create `framework/tool/process_registry.go` | ManagedProcess + Registry Start/Get/List/Poll/Log/Wait/Kill |
| Create `framework/tool/process_tool.go` | `process` 工具注册 |
| Create `framework/tool/process_*_test.go` | 单测 |
| Modify `framework/tool/terminal_tool.go` | `background` / `notify_on_complete`；Pending 携带 flags |
| Modify `framework/tool/terminal_pending.go` | Background / NotifyOnComplete |
| Modify `framework/tool/sequential.go` + `toolset.go` | `process` → terminal + sequential |
| Modify `portal/internal/chat/terminal_wiring.go` | 共享 ProcessRegistry；注册 process |
| Modify gap / hermes F2/F3 状态 | 最小集已落地 |

---

### Task 1: ProcessRegistry

- Start(chatSessionID, command, workdir, timeoutSec, notify, maxBytes) → processSessionID
- Poll(id, since) → status + new_stdout/stderr + cursor
- Log(id, offset, limit) → 分页输出
- Wait(id, timeoutSec) → 阻塞至退出或超时
- Kill(id) → 取消 + status=killed
- List(chatSessionID) → 摘要列表

- [x] TDD：start → poll running → exit → poll exited；kill 中途

### Task 2: terminal background + process tool

- terminal schema 增加 `background`、`notify_on_complete`
- background 路径：denylist/danger 同前台；spawn 后立即返回 `{status:running, session_id}`
- confirm 路径须保留 background flags（Pending 字段）
- RegisterProcessTool；action ∈ list|poll|log|wait|kill；（write/submit/close → not_implemented）

- [x] 集成测 + `go test ./tool/ -run 'Terminal|Process' -count=1`

### Task 3: Portal + docs

- wiring：`reg := NewProcessRegistry()` 注入 TerminalConfig + RegisterProcessTool
- CheckFn 与 terminal Enabled 一致
- gap S1 process 行更新；hermes F2/F3 标注最小集

- [x] Portal + docs

---

## 验收

- [x] `background=true` 立即返回 session_id，不阻塞到命令结束
- [x] poll 可增量取日志；完成后 status=exited
- [x] kill 后 status=killed，再 poll 一致
- [x] 前台路径零回归
