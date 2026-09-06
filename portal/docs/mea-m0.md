# MEA M0 / M1（Portal）

Manager–Execute–Audit 最小子集：session 旁路 TaskState JSON + rules / cascade Auditor。默认关。

**M0.5 Chat 接线**：flag/pilot/**Agent UI `mea_enabled`** 任一开启，且**成功解析出非空** `mea-checks` 或 `mea-acceptance`（仅有 fence 外观不够）、Workspace 非空时，`SendMessageStream` 走 `RunRulesMEA`。否则现网路径不变。

**端到端走读（含纯 ReAct、SSE、排障）：** [docs/superpowers/specs/2026-08-15-task-handling-current-design.md](../../docs/superpowers/specs/2026-08-15-task-handling-current-design.md)

**M1 LLM Auditor**：有 Agent model 时使用 `CascadeAuditor`（机检优先；文本 acceptance 才调 LLM）。机检失败不得被 LLM 覆盖。

## 开启方式（OR）

| 来源 | 说明 |
|------|------|
| **Agent UI** | 编辑 Agent → 运行时工具 → **MEA 长程验收**（`runtime_tools.mea_enabled`） |
| `SATH_MEA=1` | 全局强制开 |
| `SATH_MEA_PILOT_AGENTS` | 逗号分隔 agent id |

`MEAEnabledForAgent(id, agent.mea_enabled) = UI勾选 OR 全局 env OR pilot`

## 进入 MEA 的消息格式

消息末尾附加 fenced JSON（**仅 `ParseMEA*` 返回 `ok=true` 时**从持久化/送模型文本剥离；非法/空 fence 可能保留）：

| Fence | 内容 |
|-------|------|
| `mea-checks` | `AcceptanceCheck` 对象数组（机检） |
| `mea-acceptance` | 字符串数组（文本验收 → M1 LLM） |

SSE：`event: mea`（phase=`started|round|finished`）。

## 存储

- `{data_root}/mea/{session_id}.json`
- WorkDir = Agent Workspace（空则不进 MEA）

## 测试

```bash
cd framework && go test ./mea/ -count=1
cd portal && go test ./internal/chat/ -run "TestParseMEA|TestMEA|TestRunRulesMEA" -count=1
```
