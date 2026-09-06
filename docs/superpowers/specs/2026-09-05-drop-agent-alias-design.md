# S13 收口：删除 `framework/agent` 一季别名

**日期**: 2026-09-05  
**状态**: 已确认（S3 leftover；仓内已无 Go import；2026-09-05 实施）  
**范围**: 删除 `framework/agent` 别名包。不改 `harness` 语义，不删 ChatStream 包装，不拆 GrowthWorker。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S3](./2026-09-05-harness-workspace-rename-design.md)（循环已在 `harness`）

**一句话**: 骨架的名字是 `harness`；一季转发包已无仓内调用者，删掉以免旧公式 `agent` 继续占位。

---

## 1. 背景

S3：`framework/agent` → `framework/harness`，并留一季别名转发。S4–S12 的新代码禁止 import `agent`。现网磁盘事实：

- `framework/agent/` 只剩 `alias.go` + `alias_test.go`
- `rg --glob '*.go' github.com/sixath/framework/agent`（排除 `_neo4j_q`）**零命中**（别名包自身也不再被别人 import）

「一季」是给迁移窗口。仓内窗口已关。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `framework/agent` | **删除**整个包 |
| 仓内 Go import | 必须是 `github.com/sixath/framework/harness` |
| `agent "github.com/sixath/framework/harness"` 这种本地别名 | **保留**（只是标识符，不是旧包） |
| 历史 spec/plan 里的路径字符串 | **不改** |
| ChatStream 死包装 | **不删**（下一刀） |
| GrowthWorker | **不改** |

---

## 3. 行为

```text
import "github.com/sixath/framework/agent"  → 编译失败
import "github.com/sixath/framework/harness" → 唯一骨架包
```

---

## 4. 非目标

- 不删 `NewChatStreamHandler` / `NewChatAgentHandlerWithContext`（**S14 已删**）
- 不拆 GrowthWorker / FinalizeTurnForBackgroundReview
- 不删 `portal/internal/chat/procedural_binding.go`
- 不合 assembler
- 不把 Portal 里的 `agent` 标识符改名

---

## 5. 成功标准

1. `framework/agent/` 不存在。
2. 现网 `*.go`（排除 `_neo4j_q`）不含 import `github.com/sixath/framework/agent`。
3. `cd framework && go test ./harness ./workspace ./context ./model ./templates -count=1` 绿。
4. ReAct / workspace hooks 行为不变。
