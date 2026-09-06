# S23 收口：默认 Chat 不再注册 append_learning

**日期**: 2026-09-05  
**状态**: 已确认（S16–S19 leftover；P4 Growth 不进默认路径；2026-09-05 实施）  
**范围**: Portal 默认 Send / Stream / 快捷 Chat 装配。不删 `RegisterLearningTools` / `append_learning` 实现。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S22](./2026-09-05-skill-autoroute-off-design.md)；Growth 已退出默认 Chat

**一句话**: `append_learning` 是给 Growth 复盘写 `.learnings` 的器官；默认循环不要再把它塞进 registry。

---

## 1. 背景

S12–S19 把默认 Chat 从 Growth 钩子、nudge、yaml 复盘开关拆开。现网仍：

- `chat.go` Send / Stream 调用 `chat.RegisterLearningTools`
- `agent.go` 快捷 Chat 同样注册

注释写明「供 Growth 复盘消费」。Growth worker 默认不跑；工具却出现在每次 Run 的工具面，模型可以往 workspace 写 `.learnings`。P4：Growth 不进默认路径。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| Send / Stream / 快捷 Chat 的 `RegisterLearningTools` | **删除调用** |
| `chat.RegisterLearningTools` / `RegisterAppendLearningTool` | **保留**（opt-in 装配） |
| `RegisterAskUserTools` | **不改** |
| prefetch / MaybeSpill / assembler | **不改** |

---

## 3. 行为

```text
SendMessage / SendMessageStream / 快捷 Chat:
  仍装 runtime tools / ask_user / wecom
  不再 RegisterLearningTools → registry 无 append_learning

显式调用 chat.RegisterLearningTools(reg) 仍可注册
```

---

## 4. 非目标

- 不删 `framework/tool/skillops` 的 append_learning 实现
- 不改 GrowthWorker Loop
- 不合 assembler

---

## 5. 成功标准

1. `chat.go` / `agent.go` 不含 `RegisterLearningTools`。
2. `cd portal && go test ./internal/service ./internal/chat -count=1` 绿（skip 预存 SQLITE_BUSY / `TestSearchSessionsWithAgentFilterRequiresAgentUse`）。
