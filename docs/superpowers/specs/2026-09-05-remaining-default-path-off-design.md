# S24 收口：一次性拆掉剩余默认路径 leftover

**日期**: 2026-09-05  
**状态**: 已确认（用户要求一次性补齐剩余洞；2026-09-05 实施）  
**范围**: 仍在默认 Run 上执行的领域闸与平行面装配。不删 Growth/MEA/Hub/hypertool 包，不合 assembler。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S23](./2026-09-05-unwire-append-learning-design.md)

**一句话**: 默认循环里还焊着的凭据回拉、任务锁文案、记忆抽取/图谱通知、prefetch、toolFamily 索引一次拆掉。

---

## 1. 背景

S1–S23 已把调查闸文件、Growth Chat 钩子、hypertool、SQL heal、append_learning 拆出默认路径。磁盘上仍在执行：

| leftover | 现网 |
|----------|------|
| `credentialSolicitationRedirect` | `ReActAgent` 三处循环：终答前拦「索取密码」并注入回压 |
| `HasSuccessfulBoundEvidence` | 上述回拉的豁免条件（证据闸残骸） |
| `【本轮任务锁】` | `forceFinalSummary` 经 `taskLockQFromRequest` 拼进 user 文案 |
| `NotifyMemoryExtractFromTurn` / `NotifyMemoryGraphFromTurn` | Send / Stream / SaveAssistant 每轮调用（yaml 关则 no-op，装配点还在） |
| `prefetchOrchestratorForReAct` | `BuildReActAgent` 只要全局 Orchestrator 非 nil 就注入 |
| `BuildToolFamilyIndex` | Chat 传给 `McpExpandOnMiss`；map 只写不读（TurnIntentGate 残骸） |

父规格 §7.2–7.3：默认路径无领域 `PostModelPolicy`、prompt 不含【本轮任务锁】。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| 凭据回拉 | **移出 ReAct 循环**；`tool.MatchCredentialSolicitation` **保留** |
| `evidence_tools.go` | 无调用者则 **删除** |
| `AnswerOriginalQuestionPrompt` | 只返回 `ForcedFinalSummaryPrompt`，**不再拼任务锁** |
| Chat extract / graph notify | **删除调用**；函数保留（yaml opt-in 以后可再接） |
| `BuildReActAgent` prefetch | **不再注入** MemoryOrchestrator |
| `McpExpandOnMiss.ToolFamily` | **删除字段与刷新** |
| `MaybeSpill` / growth/mea/hub 包 / hypertool.go / assembler | **不改 / 不合** |

---

## 3. 非目标

- 不删 `tool_families.go`（code model 设置页仍引用族常量）
- 不删 `NotifyMemorySessionDirty`（会话 buffer / FTS）
- 不改 Channel auto_route
- 不合 assembler

---

## 4. 成功标准

1. `react_agent.go` 不含 `credentialSolicitationRedirect` / `【本轮任务锁】` / `task_lock_q`。
2. `chat.go` 不含 `NotifyMemoryExtractFromTurn` / `NotifyMemoryGraphFromTurn` / `BuildToolFamilyIndex`。
3. `agent_builder.go` 不含 `prefetchOrchestratorForReAct`。
4. `cd framework && go test ./harness ./tool -count=1` 绿。
5. `cd portal && go test ./internal/chat ./internal/service -count=1` 绿。
