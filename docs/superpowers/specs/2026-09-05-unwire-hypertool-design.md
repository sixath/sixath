# S20 收口：CLI 默认 skills handler 不再装 HyperTool

**日期**: 2026-09-05  
**状态**: 已确认（P4 leftover；2026-09-05 实施）  
**范围**: `framework/templates/skills_handler.go`。不删 `framework/tool/hypertool.go`，不改 Portal Chat 装配。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S19](./2026-09-05-growth-yaml-defaults-off-design.md)；P4（默认路径不再接线 hypertool）

**一句话**: HyperTool 是可选器官；默认 CLI skills 装配不得注册它，也不得把策略段塞进 system prompt。

---

## 1. 背景

P4：默认路径不再接线 hypertool。现网 `NewSkillsAwareChatHandlerFromConfig` 仍：

1. `tool.RegisterHyperTool`（`Enabled=false` 时不注册工具，但装配点还在）
2. `buildSkillsAwareSystemPrompt(..., cfg.HyperTool.Enabled)` 可插入 `HyperToolPromptSnippet`

配置默认 `Enabled=false`，所以现网不跑 Python 块。装配点留着，等于以后 yaml 一打开就回到默认循环。父规格 §6.3：移出默认装配，不在本规格内重写。

Portal Chat 不调用 `RegisterHyperTool`（磁盘 `rg` 仅 `templates/skills_handler.go` + 包内测试）。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `RegisterHyperTool` 在 skills handler | **删除该调用** |
| `buildSkillsAwareSystemPrompt` | **删除**；改用已有 `BuildSkillsAwarePrompt` |
| `framework/tool/hypertool.go` / `RegisterHyperTool` API | **保留**（opt-in 调用者自己装） |
| `config.HyperTool` 字段 | **保留**（本切片不删配置结构） |
| SQL heal / query spill / assembler | **不改** |

---

## 3. 行为

```text
NewSkillsAwareChatHandlerFromConfig:
  仍装 load_skill / execute_skill_script / RCA
  不再 RegisterHyperTool
  system prompt = skills.BuildSkillsAwarePrompt（无 HyperTool 段）

cfg.HyperTool.Enabled=true 对默认 skills handler 无效
显式调用 tool.RegisterHyperTool(..., Enabled: true) 仍可注册
```

---

## 4. 非目标

- 不删 hypertool 包与单测
- 不删 `HealReadSQL` / `MaybeSpill`
- 不改 Portal `agent_builder`
- 不合 assembler

---

## 5. 成功标准

1. `skills_handler.go` 不含 `RegisterHyperTool` / `HyperToolPromptSnippet`。
2. `cd framework && go test ./templates ./tool -count=1` 绿。
