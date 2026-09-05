# S21 收口：删除无调用者的 SQL heal

**日期**: 2026-09-05  
**状态**: 已确认（S1 leftover；P4 默认自愈；2026-09-05 实施）  
**范围**: `framework/tool/data/sql_heal.go` 与其单测。不改 `execute_read` 行为，不删 `MaybeSpill`。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S1](./2026-09-05-dead-code-hub-off-design.md)（已拆 `queryWithSchemaHeal`）；[S20](./2026-09-05-unwire-hypertool-design.md)

**一句话**: 默认 `execute_read` 已不自动改写失败 SQL；`HealReadSQL` / `SchemaHealHint` 零调用，删掉以免假装还能自愈。

---

## 1. 背景

P4：SQL heal 退出默认自愈。S1 删了 `queryWithSchemaHeal` 接线。现网磁盘：

- `HealReadSQL` / `SchemaHealHint` 只活在 `sql_heal.go` 与 `sql_heal_test.go`
- `TestExecuteRead_doesNotAutoHeal*` 已锁定默认路径只跑一次查询、把 schema 错误原样返回

函数留下等于「器官还在货架上」，和默认路径无关。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `sql_heal.go` / `sql_heal_test.go` | **删除** |
| `TestExecuteRead_doesNotAutoHeal*` | **保留** |
| `MaybeSpill` / `query_spill.go` | **不改**（器官落盘细节） |
| hypertool 包 / assembler | **不改** |

---

## 3. 行为

```text
HealReadSQL / SchemaHealHint → 不存在
execute_read 遇 schema 错误 → 仍原样失败，不二次改写
```

---

## 4. 非目标

- 不改 `MaybeSpill` / `result_stats.go`
- 不改 Portal Chat
- 不合 assembler

---

## 5. 成功标准

1. `framework/tool/data/sql_heal.go` 不存在。
2. 现网 `*.go`（排除 `_neo4j_q`）不含 `HealReadSQL` / `SchemaHealHint`。
3. `cd framework && go test ./tool/data ./tool -count=1` 绿。
4. `TestExecuteRead_doesNotAutoHealUnknownSelectColumn` 仍通过。
