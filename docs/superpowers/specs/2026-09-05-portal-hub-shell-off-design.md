# S29 收口：Portal Hub 管理面退出外壳

**日期**: 2026-09-05  
**状态**: 已确认（S1 leftover；用户继续清货架；2026-09-05 实施）  
**范围**: Portal `hub_*.go` / `memory_hub*.go` / `hub_wire.go` / `binding_store_mysql.go`，以及 Agent 表单/详情上的 Hub 字段。不删 `framework/memory/hub`，不 regen proto。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S1](./2026-09-05-dead-code-hub-off-design.md)；[S28](./2026-09-05-prefetch-hub-off-design.md)

**一句话**: S1 要求拆 Hub HTTP+UI；路由早就没注册，handler 和 `hub_*.go` 还躺着假装能管 loadout。删掉。

---

## 1. 背景

S1 CUT：Hub 管理面退出外壳；`framework/memory/hub` 留包。S28 切断了 memory 包根对 hub 的 import。现网磁盘（以 `Test-Path` 为准，Cursor Grep 会扫到已删文件）：

| leftover | 现网 |
|----------|------|
| Portal `hub_*.go` / `memory_hub*.go` / `hub_wire.go` / `binding_store_mysql.go` | **已不存在**；`http.go` 也不注册 `/hub/` |
| Web | Agent 表单/详情仍编辑展示 `hub_governance` / `hub_knowledge` / fallback |
| proto / DB `hub_*` | 死键，Update 仍 merge |

本刀：锁定 Go 管理面保持消失，并拆 Web 假入口。growth 仍 opt-in，**不在本刀**。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| Portal `hub_*.go` / `memory_hub*.go` / `hub_wire.go` / `hub_assets.go` / `hub_knowledge.go` | **删除** |
| `binding_store_mysql.go` | **删除**（仅 Hub 引用） |
| Web Hub 治理/知识/回落字段 | **删除** |
| proto / biz / DB `hub_*` | **保留死键**（不 regen proto；Update merge 仍保留已有值） |
| `framework/memory/hub` | **保留** |
| growth / assembler | **不改 / 不合** |

---

## 3. 行为

```text
GET /api/v1/memory-hub/catalog 与 /agents/{id}/hub/* → 仍不注册
portal 不再 import github.com/sixath/framework/memory/hub
Agent 表单 / 详情不再编辑或展示 Hub 治理/知识
已有 Agent 的 hub_* JSON 不被本刀擦掉
```

---

## 4. 非目标

- 不删 `framework/memory/hub`
- 不删 growth
- 不改 Channel、`MaybeSpill`
- 不删 `scripts/seed_hub_graph.go`（Neo4j 种子，不是 Memory Hub 管理面）
- 不合 assembler

---

## 5. 成功标准

1. `portal/internal/chat/hub_bootstrap.go`、`portal/internal/server/memory_hub.go`、`portal/internal/service/hub_wire.go` 不存在。
2. `http.go` 不含 `/hub/` 与 `memory-hub`。
3. 现网 Portal `*.go`（排除 `_neo4j_q`）不含 `github.com/sixath/framework/memory/hub`。
4. Agent 表单/详情不含 `hub-governance` / `hub-knowledge` testid。
5. `cd portal && go test ./internal/chat ./internal/service ./internal/server ./internal/data -count=1` 绿（skip 预存 SQLITE_BUSY）。
