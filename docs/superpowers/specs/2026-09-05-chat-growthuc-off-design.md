# S17 收口：ChatService 不再注入 `growthUC`

**日期**: 2026-09-05  
**状态**: 已确认（S16 leftover；2026-09-05 实施）  
**范围**: `ChatService` 构造函数与 `wire_gen`。不删 `ProvideGrowthUsecase`（worker/curator 仍用）。不改 CuratorWorker。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S16](./2026-09-05-background-review-off-design.md)（Chat 已无 after-turn C3）

**一句话**: 默认对话服务不再持有 GrowthUsecase；成长器官只挂在 opt-in worker / curator 上。

---

## 1. 背景

S16 拆掉 ChatService C3 钩子后，`growthUC` 只在构造时赋值，**没有任何方法读取**。S16 故意不改签名以免 regen wire。本刀补上。

CuratorWorker 已按 `curator_enabled=false` 返回 nil，**不在本刀范围**。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `ChatService.growthUC` | **删除** |
| `NewChatService` / `NewChatServiceWithMemoryStore` / `newChatService` / `ProvideChatServiceWithTurnTrace` | 去掉 `growthUC` 参数 |
| `wire_gen.go` | 同步调用；**仍** `ProvideGrowthUsecase` 给 worker/curator |
| CuratorWorker | **不改** |
| `framework/growth` | **不删** |
| assembler | **不合** |

---

## 3. 行为

```text
NewChatService(...) 不再接收 GrowthUsecase
默认 DeleteSession 仍不注册 growth session-end hooks
growth.worker_enabled / curator_enabled 仍可构造各自 worker
```

---

## 4. 非目标

- 不改 CuratorWorker 门控
- 不改 `DefaultNudgeConfig`
- 不合 assembler

---

## 5. 成功标准

1. `chat.go` 不含 `growthUC`。
2. `ProvideChatServiceWithTurnTrace` 参数列表不含 `*biz.GrowthUsecase`。
3. `cd portal && go test ./internal/service ./cmd/backend -count=1` 绿（skip 预存 SQLITE_BUSY）。
4. `TestDeleteSession_DefaultChatService_noGrowthSessionEndHooks` 仍绿。
