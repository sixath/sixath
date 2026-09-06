# S12 收口：默认 Run 不再经 `growthReActOptions`

**日期**: 2026-09-05  
**状态**: 已确认（P4 leftover；2026-09-05 实施）  
**范围**: Portal 默认 Chat/Stream/快捷 Chat 装配。不删 `framework/growth` 包，不改 BackgroundReview worker。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: P4（默认路径不再接线 growth）

**一句话**: `hooks.yaml` / FailureCapture 是 Harness 器官，不要再藏在名叫 growth 的选项里；默认循环上已无调用的 Growth 钩子删掉。

---

## 1. 背景

P4：默认路径不再接线 growth。现网 `chat.go` 仍 `append(s.growthReActOptions(...))`。该函数实际只做两件事：

1. `LoadWorkspaceHarnessHooks`（`workspace/harness/hooks.yaml`）
2. 可选 `FailureCaptureHook`（`SATH_GROWTH_FAILURE_CAPTURE`）

真正的 Growth 循环钩子（`growthToolSuccessHook` / `notifyGrowthAssistantTurn` / `registerGrowthSessionHooks`）**已无默认调用者**。快捷 Chat（`agent.go`）只走 `HarnessReActOptions`，因此今天还吃不到 workspace hooks。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| workspace hooks + 可选 FailureCapture | 并进 `chat.HarnessReActOptions` |
| `growthReActOptions` | **删除**；chat.go 不再调用 |
| 无调用者的 Growth 钩子 | 删 `growthToolSuccessHook` / `notifyGrowthAssistantTurn` / `registerGrowthSessionHooks` / `runGrowthAsync`；可删 `growth_chat.go` |
| BackgroundReview / GrowthWorker | **不改** |
| `framework/growth` 包 | **不删** |
| FailureCapture 语义 | 仍 opt-in；只是装配点换地方（快捷 Chat 也会吃到，与主对话一致） |

---

## 3. 行为

```text
HarnessReActOptions(ws, extraSkills):
  WithReActWorkspace
  WithReActSkillsDirs（若有）
  LoadWorkspaceHarnessHooks(ws) + 可选 FailureCapture → WithReActToolHooks

SendMessage / Stream / 快捷 Chat:
  只走 HarnessReActOptions，不再 append growthReActOptions
```

---

## 4. 非目标

- 不删 `framework/agent` 别名
- 不删 `NewChatStreamHandler`
- 不拆 GrowthWorker / FinalizeTurnForBackgroundReview
- 不合 assembler

---

## 5. 成功标准

1. `HarnessReActOptions` 在有 `harness/hooks.yaml` 时带上 ToolHooks。
2. `chat.go` 不含 `growthReActOptions`。
3. 默认 `DeleteSession` 仍不注册 session-end Growth hooks。
4. `cd portal && go test ./internal/chat ./internal/service -count=1` 绿（skip 预存 SQLITE_BUSY）。
