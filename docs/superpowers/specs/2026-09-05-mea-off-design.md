# S27 收口：删除无调用者的 MEA 包

**日期**: 2026-09-05  
**状态**: 已确认（用户继续清货架；器官包第二刀；2026-09-05 实施）  
**范围**: `framework/mea/` 整包。不删 growth / hub，不改 Channel。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S26](./2026-09-05-hypertool-off-design.md)；P4（MEA 退出默认路径）

**一句话**: Portal 已不 import MEA；整包零调用，删掉以免假装还能旁路 ReAct。

---

## 1. 背景

父规格 §6.3：`framework/mea/` 移出默认装配、不重写。P4 / 后续切片拆了 Portal `mea_*.go` 与 `streamWithRulesMEA`。现网磁盘：

- `framework/mea` 仍在，`go list ./mea` 成功
- 仓内无 `github.com/sixath/framework/mea` 的生产或测试 import（排除包自身）
- Portal `mea_run.go` / `mea_stream.go` 已不存在

growth / hub 仍有 opt-in 或 prefetch 引用，**不在本刀**。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `framework/mea/` | **整目录删除** |
| growth / hub / assembler | **不改 / 不合** |

---

## 3. 行为

```text
github.com/sixath/framework/mea → 不存在
默认 Chat / CLI 仍不旁路 ReAct 跑 MEA
```

---

## 4. 非目标

- 不删 `framework/growth` / `memory/hub`
- 不改 `MaybeSpill`
- 不合 assembler

---

## 5. 成功标准

1. `framework/mea` 目录不存在。
2. 现网 `*.go`（排除 `_neo4j_q`）不含 `github.com/sixath/framework/mea`。
3. `cd framework && go test ./harness ./tool ./templates -count=1` 绿。
