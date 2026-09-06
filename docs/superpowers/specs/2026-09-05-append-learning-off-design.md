# S33 收口：删除无调用者的 append_learning 工具

**日期**: 2026-09-05  
**状态**: 已确认（用户继续清货架；skillops 对 growth 第一刀；2026-09-05 实施）  
**范围**: `RegisterAppendLearningTool` 与 Skills prompt 中教模型调用它的句子。不删 `growth.AppendLearning`，不改 `skill_manage`。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S23](./2026-09-05-unwire-append-learning-design.md)；[S25](./2026-09-05-shelf-family-code-model-off-design.md)（删 `RegisterLearningTools`）；[S32](./2026-09-05-failure-capture-off-design.md)

**一句话**: 默认 Chat 已不注册 `append_learning`；实现零调用，prompt 还在教模型调它。删掉。

---

## 1. 背景

S23 拆了默认装配；S25 删了 Portal 包装函数。现网：

- `RegisterAppendLearningTool` 只活在 `learnings_tools.go` 与其测试
- `BuildSkillsAwarePrompt` 仍写「请调用 append_learning」
- `skill_manage` 仍深绑 growth（lease/patch/pin），**不在本刀**

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `learnings_tools.go` / `_test.go` | **删除** |
| Skills prompt 中 append_learning 教唆句 | **删除** |
| `growth.AppendLearning` / DiscoverLearningsDirs | **保留**（worker / 以后器官） |
| `skill_manage` / assembler | **不改 / 不合** |

---

## 3. 行为

```text
RegisterAppendLearningTool → 不存在
BuildSkillsAwarePrompt 不再提到 append_learning
默认 Chat 仍无该工具
```

---

## 4. 非目标

- 不删 `framework/growth`
- 不改 skill_manage 对 growth 的 lease/patch
- 不改 opt-in GrowthWorker
- 不合 assembler

---

## 5. 成功标准

1. `framework/tool/skillops/learnings_tools.go` 不存在。
2. 现网 `*.go`（排除 `_neo4j_q`）不含 `RegisterAppendLearningTool`。
3. `BuildSkillsAwarePrompt` 不含 `append_learning`。
4. `cd framework && go test ./tool/skillops ./skills ./templates -count=1` 绿。
5. `TestChatGo_DoesNotRegisterLearningTools` 仍通过。
