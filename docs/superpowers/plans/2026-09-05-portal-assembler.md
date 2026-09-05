# Portal Assembler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Portal chat 只装配 Model + 已绑定器官 + Workspace + Agent `systemPrompt`；删除调查闸 / 任务锁 / turn-surface / Skill 全文预注入 / Prompt 叠层。无调用者则删除 `PostModelPolicy`。

**Architecture:** `SendMessage` / Stream / `Agent.Chat` 的 registry = 绑定全集（不再 `PrepareTurnToolSurface`）。system prompt = `BuildEffectiveSystemPrompt`（Agent 文案 + skills **索引**，不是 SKILL 全文）+ `ask_user` 指引 + 企微绑定说明。不删 `mea_*.go` / Hub。

**Tech Stack:** Go（`portal/internal/chat`、`portal/internal/service`、`framework/agent`）

**规格:** [`2026-09-05-agent-model-workspace-harness-design.md`](../specs/2026-09-05-agent-model-workspace-harness-design.md) §6.2 / §11 P3

---

## File map

**删除（及测试）：** `investigation_gates.go`、`turn_intent_gate.go`、`task_lock.go`、`turn_surface.go`、`http_grounding.go`、`intent_resolver.go`、`intent_classifier.go`、`catalog_prompt.go`、`web_prompt.go`、`code_analysis_prompt.go`、`datasource_prompt.go`、`turn_intent_prompt.go`、`skill_router.go`（预注入）、`framework/agent/post_model_policy.go`

**保留：** `mea_*.go`、`hub_*.go`、`procedural_binding.go`、`tool_families.go`（`ShouldApplyEvidenceGate` / P4）、`evidence_tools.go`、`plan_agent.go`、`ask_user`、Confirm、Growth 接线

**改：** `service/chat.go`、`service/agent.go`、`agent_builder.go`（去掉 `WithReActPostModelPolicy`）、`cmd/backend/main.go`、`conf/chat_config.go`、`react_agent.go`（去掉 `applyPostModelPolicy`）

禁止：`_neo4j_q/`；不删 MEA/Hub 包。

---

### Task 1: 拆掉 chat 装配上的闸与叠层

三处 prompt 改为：

```go
effectivePrompt := chat.BuildEffectiveSystemPrompt(agentMeta.SystemPrompt, skillsIdx)
effectivePrompt = chat.AppendAskUserToolPrompt(effectivePrompt)
effectivePrompt = appendWecomBoundSystemPrompt(ctx, s.channelUC, effectivePrompt, agentMeta)
```

`BuildRegistry` / `RegisterAgentRuntimeTools` / `McpExpandOnMiss` 不再传 `ActiveFamilies`。删除 `PrepareTurnToolSurface`、`TurnIntentGateOption`、task lock metadata。

### Task 2: 删文件并修编译

按 File map `git rm`。`evalgolden` 只留 MEA / `ShouldApplyEvidenceGate`；断言 prompt 不含「本轮任务锁」。`catalog_integration` 不再要求目录块置顶。

### Task 3: 删除 `PostModelPolicy`

Portal 无调用者后：删接口、三处循环调用、`post_model_policy_test.go`。保留 `credentialSolicitationRedirect`。

### Task 4: 配置开关删除

`chat.investigation_gates` / `SATH_INVESTIGATION_GATES` / `SATH_TURN_*` / `SATH_TASK_LOCK`。`main.go` 不再 `ApplyInvestigationGates`。YAML 去掉该键。

### Task 5: 回归

```
cd framework && go test ./agent ./tool ./model ./events ./mea -count=1
cd portal && go test ./internal/chat/... ./internal/service/... ./internal/conf/... ./internal/biz/... -count=1 -skip TestNotifySessionMessageIndexed_WithDetachedCaller
```

验收：无 `PrepareTurnToolSurface`；无任务锁文案；`BuildReActAgent` 不挂 PostModelPolicy；MEA/Hub 文件仍在。
