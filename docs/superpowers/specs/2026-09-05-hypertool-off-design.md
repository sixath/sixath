# S26 收口：删除无调用者的 HyperTool

**日期**: 2026-09-05  
**状态**: 已确认（用户继续清货架；器官包第一刀；2026-09-05 实施）  
**范围**: `framework/tool/hypertool.go`、runner 与其单测。不删 `config.HyperTool` 死键，不删 growth/mea/hub。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S20](./2026-09-05-unwire-hypertool-design.md)；[S25](./2026-09-05-shelf-family-code-model-off-design.md)

**一句话**: 默认 CLI 已不注册 HyperTool；实现零调用，删掉以免 yaml 打开就假装还能装。

---

## 1. 背景

S20 拆了 `skills_handler` 装配。现网 `RegisterHyperTool` 只活在 `hypertool.go` 与包内测试。`cfg.HyperTool.Enabled=true` 对默认入口无效。

growth / mea / hub 仍有 opt-in 或 prefetch 引用，**不在本刀**。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `hypertool.go` / `hypertool_test.go` / `hypertool_runner.py` | **删除** |
| `templates/hypertool_off_test.go` | **保留**（锁定 skills handler 仍不接线） |
| `config.HyperTool` | **保留死键**（不改 yaml 解析） |
| growth / mea / hub / assembler | **不改 / 不合** |

---

## 3. 行为

```text
RegisterHyperTool / HyperToolPromptSnippet → 不存在
默认 skills handler 仍不注册 hypertool
config.yaml hypertool 字段仍能解析、无效
```

---

## 4. 非目标

- 不删 `framework/growth` / `mea` / `memory/hub`
- 不改 `MaybeSpill`
- 不合 assembler

---

## 5. 成功标准

1. `framework/tool/hypertool.go` 不存在。
2. 现网 `*.go`（排除 `_neo4j_q`）不含 `RegisterHyperTool`。
3. `cd framework && go test ./tool ./templates -count=1` 绿。
4. `TestSkillsHandlerGo_doesNotWireHyperTool` 仍通过。
