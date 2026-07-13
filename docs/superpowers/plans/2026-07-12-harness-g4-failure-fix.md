# G4：失败模式 → harness 修复流水线 MVP

> **For agentic workers:** Use writing-plans / execute directly. Skip git commits unless asked.

**Goal:** 工具失败自动沉淀到 `.learnings/ERRORS.md`；交互路径用 `harness-fix` Skill + `skill_manage` 人审确认后落 Skill。不改 ReAct 内核；不做持久化 Hook（G4.1）。

**Architecture:** `FailureCaptureHook`（ToolHook.After）→ ERRORS.md → Agent 加载 `harness-fix` → `skill_manage` create/patch → SSE `confirm_required` → ApplyPatchBatch。

**非目标：** GrowthWorker 自动 Apply 行为变更；`hooks.yaml`；主循环 fork fix agent；DB failure_pattern 表。

---

## 文件

| 文件 | 职责 |
|------|------|
| Create `framework/growth/learnings_append.go` | `AppendErrorLearning` 共享写盘 |
| Create `framework/agent/failure_capture_hook.go` | After 捕获 hard/soft 失败 |
| Create `framework/skills_examples/skills/harness-fix/` | SOP Skill |
| Modify `skillops` skill_manage | `RequirePatchConfirm` |
| Modify portal chat wiring | 默认关捕获；主对话 patch confirm |
| Modify gap G4 | 已落地（MVP）|

## 验收

- [x] 默认关闭零回归
- [x] 开启后失败写入 ERRORS.md（去重）
- [x] 主对话 patch/create 须 confirm_token
- [x] Growth fork 仍可直写 patch
- [x] Index 可发现 `harness-fix`
