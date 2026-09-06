# S30 收口：删除无调用者的 Memory Hub 包

**日期**: 2026-09-05  
**状态**: 已确认（用户继续清货架；器官包第三刀；2026-09-05 实施）  
**范围**: `framework/memory/hub/` 整树。不删 growth，不 regen proto。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S28](./2026-09-05-prefetch-hub-off-design.md)；[S29](./2026-09-05-portal-hub-shell-off-design.md)

**一句话**: Portal 与 memory 包根已不 import hub；整树零外部调用，删掉以免假装还能 ResolveLoadout。

---

## 1. 背景

父规格 P4 / S1：Hub 退出默认 Run 与管理面，包先留下。S28 拆了 prefetch import；S29 锁定 Portal `hub_*.go` 已不存在并拆 Web 字段。现网磁盘（`Test-Path` / `rg`，排除 `_neo4j_q` 与包自身）：

- `framework/memory/hub` 仍在
- 仓内无 `github.com/sixath/framework/memory/hub` 的外部 import

growth 仍有 Portal opt-in，**不在本刀**。proto / DB `hub_*` 死键留下。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `framework/memory/hub/` | **整目录删除**（含 `local/`、`fake/`） |
| proto / biz `hub_*` | **保留死键** |
| growth / assembler | **不改 / 不合** |

---

## 3. 行为

```text
github.com/sixath/framework/memory/hub → 不存在
默认 Chat 仍不装配 Hub Catalog / knowledge_* 工具
```

---

## 4. 非目标

- 不删 `framework/growth`
- 不改 `MaybeSpill`、Channel
- 不 regen proto
- 不合 assembler

---

## 5. 成功标准

1. `framework/memory/hub` 目录不存在。
2. 现网 `*.go`（排除 `_neo4j_q`）不含 `github.com/sixath/framework/memory/hub`。
3. `cd framework && go test ./memory ./harness ./tool ./templates -count=1` 绿。
