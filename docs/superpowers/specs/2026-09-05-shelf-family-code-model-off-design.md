# S25 收口：零调用货架 + FamilyCode 切模

**日期**: 2026-09-05  
**状态**: 已确认（用户选 C；2026-09-05 实施）  
**范围**: 默认路径已拆、现零调用的 Portal 货架函数；turn-surface 留下的 tool family / code 切模及其外壳。不删 growth/mea/hub/hypertool 包。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S24](./2026-09-05-remaining-default-path-off-design.md)

**一句话**: 删掉假装还能用的零调用函数，并拆掉 FamilyCode 切模——turn-surface 没了之后它不会点火。

---

## 1. 背景

S24 从默认 Run 拆了接线，函数和 family 文件还在。磁盘事实：

| leftover | 现网 |
|----------|------|
| `prefetchOrchestratorForReAct` | 无调用者；ChatService 仍 `RebuildPrefetchMemoryOrchestrator`，结果无人注入 ReAct |
| `RegisterLearningTools` | 无调用者；`RegisterAppendLearningTool` 仍在 skillops |
| `NotifyMemoryExtractFromTurn` / `NotifyMemoryGraphFromTurn` | 无调用者；yaml 配置仍写入、不再跑 |
| `tool_families.go` | 生产调用只剩 `ResolveTurnModel` 的 `FamilyCode` |
| `ResolveTurnModel` | Chat / Stream / builder **零调用** |
| 设置页 / Agent 表单「源码分析模型」 | 只为上述切模供数 |

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `prefetchOrchestratorForReAct` | **删除**；ChatService / `SetPortalAgentExtra` **不再 Rebuild** |
| `BuildPrefetchMemoryOrchestrator` | **保留**（测试与以后 opt-in） |
| `RegisterLearningTools` | **删除**；不删 skillops `append_learning` |
| extract / graph `Notify*` | **删除函数**；保留 `SetMemory*Config` 与 pipeline 单测 |
| `tool_families.go` | **删除** |
| `ResolveTurnModel` / `code_model.go` | **删除** |
| HTTP `GET/PUT /settings/code-model` | **删除** |
| Web 源码模型表单与详情展示 | **删除** |
| proto / DB `code_model` 列 | **保留死键**（不 regen proto） |
| growth / mea / hub / hypertool / `MaybeSpill` | **不删** |

---

## 3. 行为

```text
默认 Chat 模型不再按 FamilyCode 切换
/settings 不再出现源码分析模型
Agent 表单 / 详情不再编辑或展示 code_* 切模
RebuildPrefetch 不再在装配时执行
```

---

## 4. 非目标

- 不删 `framework/growth` / `mea` / `memory/hub` / `hypertool.go`
- 不改 Channel auto_route
- 不删 `NotifyMemorySessionDirty`
- 不合 assembler

---

## 5. 成功标准

1. `portal/internal/chat/tool_families.go` 与 `code_model.go` 不存在。
2. `http.go` 不含 `/settings/code-model`。
3. `agent_builder.go` 不含 `RegisterLearningTools`。
4. `chat.go` 不含 `RebuildPrefetchMemoryOrchestrator`。
5. `cd portal && go test ./internal/chat ./internal/service ./internal/server -count=1` 绿（可 skip 预存 SQLITE_BUSY）。
