# Slim Harness Strip Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 从 ReAct 循环拆除规格 §6.1 领域短语闸与 `code_workset` 默认注入，并拆光 Portal 对已删类型的编译引用；调查闸 / turn-surface / task lock 留给 P3。

**Architecture:** 终答路径不再调用 `checkAnswerGates`；工具结果只走 `appendToolResultMessages`。`ReActConfig` 去掉 `EvidenceGate` / `CodeClaimGate`。`PostModelPolicy`、`plan_agent.go`、`evidence_tools.go`、`ShouldApplyEvidenceGate` 保留。Portal 只删 `WithReActEvidenceGate` / `CodeClaimGateTurnOption` 接线。

**Tech Stack:** Go（`framework/agent`、`portal/internal/chat`、`portal/internal/service`）

**规格:** [`2026-09-05-agent-model-workspace-harness-design.md`](../specs/2026-09-05-agent-model-workspace-harness-design.md) §6.1 / §11 P1

---

## File map

| Path | 本计划动作 |
|------|------------|
| `framework/agent/react_agent.go` | 去掉闸注入、workset、config 字段与 With* |
| `framework/agent/code_workset.go` + `_test.go` | 删除 |
| `framework/agent/{inbound,evidence,empty_idle,empty_hit_speak,truncated_page,scenario_path,surrogate_source}_gate*.go` | 删除 |
| `framework/agent/code_quote_gate.go` + `_test.go` | 删除 |
| `framework/agent/code_claim_auditor.go` + `_test.go` | 删除 |
| `framework/agent/code_claim_gate_react_test.go` | 删除 |
| `framework/agent/evidence_gate.go` + `evidence_gate_test.go` + `evidence_gate_react_test.go` | 删除 |
| `framework/agent/evalgolden_test.go` | 删除闸相关用例，或整文件若只测闸 |
| `framework/agent/react_agent_test.go` | 空闲闸用例改为「不注入」 |
| `framework/agent/evidence_tools.go` | **不改**（P3 仍引用） |
| `framework/agent/post_model_policy.go` | **不改** |
| `framework/agent/plan_agent.go` | **不改** |
| `portal/internal/chat/agent_builder.go` | 去掉 Evidence/CodeClaim 装配 |
| `portal/internal/service/chat.go` / `agent.go` | 去掉 TurnOption 调用 |
| `portal/internal/chat/agent_builder_react_opts_test.go` | 删/改引用已删 API 的测试 |
| `framework/mea/rules_auditor.go` | P1 最小编译修复：`empty_hit_speak` **fail-open**（闸已删，P4 再拆 MEA） |
| `framework/mea/evalgolden_test.go` | 去掉依赖空击否认的用例 |

禁止：删 `turn_intent_gate.go`、`task_lock.go`、`investigation_gates.go`、`mea_*.go`、`ShouldApplyEvidenceGate`。禁止 `_neo4j_q/` 夹具。测试在模块根执行：`cd framework` / `cd portal`。

---

### Task 1: 工具写回不再插入 `[code_workset]`

**Files:**
- Modify: `framework/agent/react_agent.go`（三处 `appendToolResultsWithWorkset` → `appendToolResultMessages`；删除 `appendToolResultsWithWorkset`）
- Delete: `framework/agent/code_workset.go`、`framework/agent/code_workset_test.go`
- Test: `framework/agent/react_agent_test.go`（追加）

- [ ] **Step 1: 写失败测试**

在 `react_agent_test.go` 追加：

```go
func TestReActAgent_toolResultsDoNotInsertCodeWorkset(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{
			{
				Used:      true,
				ToolName:  "calculator_add",
				Arguments: map[string]any{"a": float64(1), "b": float64(1)},
			},
			{Used: false},
		},
		plainReplies: []string{"done"},
	}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	react := NewReActAgent(fake, mem, reg, WithReActMaxSteps(5))
	_, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "1+1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range fake.lastToolMessages {
		if strings.Contains(m.Content, "[code_workset]") {
			t.Fatalf("workset card must not appear: %#v", m)
		}
	}
}
```

（若 `fakeOpenAIClient` 没有 `lastToolMessages`，用已有 EmptyIdle 测试同款字段；没有则断言 `resp` 历史/metadata 不含 `[code_workset]`。先读 `fakeOpenAIClient` 再抄字段名。）

- [ ] **Step 2: 跑测试确认失败或确认现网会注入**

Run: `cd framework && go test ./agent -run TestReActAgent_toolResultsDoNotInsertCodeWorkset -count=1`

Expected: 若循环已注入 workset 则 FAIL；计算器路径可能本来就不注入（CollectCodeWorkset 空）。**仍执行后续删除**——规格要求拆除函数，不能因为计算器路径碰巧不注入而留下 `appendToolResultsWithWorkset`。

- [ ] **Step 3: 最小实现**

`react_agent.go` 三处：

```go
messages = appendToolResultMessages(messages, records)
```

删除 `appendToolResultsWithWorkset`。删除 `code_workset.go` 与 `code_workset_test.go`。Grep `CollectCodeWorkset` / `upsertCodeWorksetMessage` / `OriginCodeWorkset`：agent 包内引用必须清零。`model` 若仅有 Origin 常量，可留到以后（本任务不强制删 model 常量）。

- [ ] **Step 4: 跑测试**

Run: `cd framework && go test ./agent -count=1`

Expected: PASS（本任务相关）。若其它测试引用 `CollectCodeWorkset`，一并删那些测试。

- [ ] **Step 5: Commit**

```bash
git add framework/agent
git commit -m "$(cat <<'EOF'
refactor(agent): stop injecting code_workset into the ReAct loop

Workspace context should come from tools and files, not a welded RCA system card.
EOF
)"
```

Windows PowerShell 无 HEREDOC 时用：

```powershell
git add framework/agent
git commit -m "refactor(agent): stop injecting code_workset into the ReAct loop"
```

---

### Task 2: 空闲终答不再二次回压

**Files:**
- Modify: `framework/agent/react_agent_test.go`（`TestReActAgent_EmptyIdleAfterToolsInjectsThenAnswers`）

- [ ] **Step 1: 把成功用例改成目标行为（先红）**

将 `TestReActAgent_EmptyIdleAfterToolsInjectsThenAnswers` 改为：工具跑完后模型返回空正文 → **直接结束**，不再注入「没有写出给用户看的正文」，`EmptyIdleNudges` 保持 0。`TestReActAgent_EmptyIdleWithoutToolsFinishes` 已符合目标，保留。

目标断言（按现网 fake 字段微调）：

```go
if fake.toolCalls != 2 {
    t.Fatalf("toolCalls=%d want 2 (no idle inject round)", fake.toolCalls)
}
trace, _ := resp.Metadata["trace"].(*RunTrace)
if trace != nil && trace.EmptyIdleNudges != 0 {
    t.Fatalf("EmptyIdleNudges=%d want 0", trace.EmptyIdleNudges)
}
```

空 `resp.Text` 可接受（不再为了填空而多一轮）。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd framework && go test ./agent -run TestReActAgent_EmptyIdleAfterTools -count=1`

Expected: FAIL（仍会 inject，`toolCalls>=3` 或 Nudges==1）

- [ ] **Step 3: 先不要改生产代码**（下一步 Task 3 拆闸时会一起绿）

若 Task 2 与 Task 3 同一提交也可以；不要在未拆 `checkAnswerGates` 前强行让空闲测试绿。

---

### Task 3: 循环去掉 `checkAnswerGates`

**Files:**
- Modify: `framework/agent/react_agent.go`
  - tools 循环 `!stepInfo.Used` 分支（约 400–417）
  - stream/sync 另外两处同样块（约 728、857）
  - `forceFinalSummary` / `forceFinalSummaryStream`（约 988–1000）
  - 删除 `shouldBufferCodeClaim` 及调用（约 824）
  - 删除 `checkAnswerGates`、`checkEvidenceGate`、`checkCodeClaimGate`、`applyAnswerGateInject`、`applyAnswerGateMeta`、`gateDoneMeta`、`evidenceGateCheck`、`collectEvidenceRefsFromTrace`、`markCodeClaimMismatch`（若只被闸使用）
- Keep: `applyPostModelPolicy`、`credentialSolicitationRedirect`

- [ ] **Step 1: 终答分支改成直接返回**

`!stepInfo.Used` 在 credential redirect 之后：

```go
lastAnswer = gen.Text
_ = a.storeAssistant(ctx, lastAnswer)
emit(events.RunCompleted, map[string]any{"text_length": len(lastAnswer), "tool_calls": len(trace.ToolCalls)})
return responseWithTrace(lastAnswer, gen.TokenUsage, trace, messages), nil
```

上面的 `return` **只适用于同步 `Run()`**。stream / events 路径只删 `checkAnswerGates` 注入/Halt，保留现有 `send(delta)` + `streamDoneEvent`，不要把同步 return 贴进 stream。

`shouldBufferCodeClaim` 若让循环多缓冲一轮，删除该分支，按无 buffer 路径走。

- [ ] **Step 2: 跑空闲测试应变绿**

Run: `cd framework && go test ./agent -run 'TestReActAgent_EmptyIdle|TestReActAgent_toolResultsDoNotInsertCodeWorkset|TestReActAgent_MaxStepsForcedSummary' -count=1`

Expected: PASS

- [ ] **Step 3: Commit**（可与 Task 4 合并，若编译仍引用闸类型则先做 Task 4）

---

### Task 4: 删除闸文件与闸测试

**Files — 删除：**

```
framework/agent/inbound_gate.go
framework/agent/inbound_gate_test.go
framework/agent/evidence_gate.go
framework/agent/evidence_gate_test.go
framework/agent/evidence_gate_react_test.go
framework/agent/code_claim_auditor.go
framework/agent/code_claim_auditor_test.go
framework/agent/code_quote_gate.go
framework/agent/code_quote_gate_test.go
framework/agent/empty_idle_gate.go
framework/agent/empty_hit_speak_gate.go
framework/agent/truncated_page_gate.go
framework/agent/truncated_page_gate_test.go
framework/agent/scenario_path_gate.go
framework/agent/scenario_path_gate_test.go
framework/agent/surrogate_source_gate.go
framework/agent/surrogate_source_gate_test.go
framework/agent/code_claim_gate_react_test.go
```

`framework/agent/evalgolden_test.go`：删除所有 `Evaluate*Gate` / `checkAnswerGates` 用例。**保留** `TestEvalGolden_e9d4`（凭据重定向，不是闸）。若删完只剩无关用例则留文件。

- [ ] **Step 1: 删除上表文件**

- [ ] **Step 2: Grep 确认 agent 包内无残留**

```
EvaluateInboundCompletenessGate
EvaluateEvidenceGate
EvaluateCodeClaim
EvaluateCodeQuoteGate
EvaluateEmptyHitSpeakGate
EvaluateTruncatedPageGate
EvaluateScenarioPathGate
EvaluateSurrogateSourceGate
WithReActEvidenceGate
WithReActCodeClaimGate
EvidenceGateConfig
CodeClaimGateConfig
ErrEvidenceGateHalt
checkEmptyIdleGate
checkEmptyHitSpeakGate
checkTruncatedPageGate
```

允许残留：`HasSuccessfulBoundEvidence`、`IsBoundEvidenceTool`、`IsSkillsFamilyToolName`（`evidence_tools.go`）。

- [ ] **Step 3: 从 ReActConfig 去掉字段**

删除：

```go
EvidenceGate EvidenceGateConfig
CodeClaimGate CodeClaimGateConfig
```

以及 `WithReActEvidenceGate`、`WithReActCodeClaimGate`、`EvidenceGateEnabled`、`CodeClaimGateEnabled`。

`PostModelPolicy` **留下**。

- [ ] **Step 3b: MEA 编译修复（不删 mea 包）**

`framework/mea/rules_auditor.go` 的 `case "empty_hit_speak"` 改为 fail-open（闸函数已不存在）：

```go
case "empty_hit_speak":
    // P1: answer-gate removed from harness; MEA check is a no-op until P4.
    return true, "skipped", nil
```

Grep `framework/mea` 对其它 `agent.Evaluate*` 的引用并同样 fail-open。改掉 `framework/mea/evalgolden_test.go` 里期望空击**否认**的断言（改为 skipped/pass，或删除该用例）。

Run: `cd framework && go test ./mea -count=1`

Expected: PASS

- [ ] **Step 4: 编译 agent 测试**

Run: `cd framework && go test ./agent -count=1`

Expected: PASS

`events.EvidenceIncomplete` 常量可留（无人 emit 即可）。

- [ ] **Step 5: Commit**（与 Task 5 **同一提交**，否则 portal 引用已删符号编不过）

```
fix(agent): remove welded answer gates from the ReAct skeleton
```

不要在 Task 5 完成前单独 push/commit Task 4。

---

### Task 5: Portal 拆掉对已删类型的引用

**Files:**
- Modify: `portal/internal/chat/agent_builder.go`
- Modify: `portal/internal/service/chat.go`（约 445–446、764–765）
- Modify: `portal/internal/service/agent.go`（约 377）
- Modify: `portal/internal/chat/agent_builder_react_opts_test.go`
- Modify: `portal/internal/chat/evalgolden_test.go`

- [ ] **Step 1: `BuildReActAgent` 不再 Enable EvidenceGate**

删除：

```go
if ShouldEnableEvidenceGate(reg) {
    opts = append(opts, agent.WithReActEvidenceGate(...))
}
```

`NewTurnIntentGate` / `WithReActPostModelPolicy` **保留**（P3）。

- [ ] **Step 2: 删除或掏空 TurnOption**

删除函数 `EvidenceGateTurnOption`、`CodeClaimGateTurnOption`、`ShouldEnableCodeClaimGate`（仅被 CodeClaim 使用时）。

**保留** `ShouldApplyEvidenceGate`、`ShouldEnableEvidenceGate`（mea / 现有启发式测试）。

`chat.go` / `agent.go` 去掉：

```go
chat.EvidenceGateTurnOption(...),
chat.CodeClaimGateTurnOption(...),
```

- [ ] **Step 3: 测试**

删除或改写：

- `TestEvidenceGateTurnOption_*`
- `TestCodeClaimGateTurnOption_*`
- `TestShouldEnableCodeClaimGate`
- `TestBuildReActAgent_enablesEvidenceGateForJaeger`（断言 `EvidenceGateEnabled` / Soft inject / `evidence_incomplete`）
- `TestBuildReActAgent_noEvidenceGateWithoutRCATools`（调用 `EvidenceGateEnabled()`）
- `evalgolden_test.go` 里对 `EvidenceGateTurnOption` / `EvidenceGateEnabled()` 的断言

可把 Jaeger builder 测试改成：**仍能 `Run` 出终答、不再 Soft inject**（`EvidenceNudges==0`，无 `evidence_incomplete`）。不要断言已删的 `EvidenceGateEnabled()`。

`TestShouldEnableEvidenceGate`、`TestShouldApplyEvidenceGate` **保留**。

- [ ] **Step 4: 跑 portal 测试**

Run: `cd portal && go test ./internal/chat/... ./internal/service/... -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```
fix(portal): stop wiring deleted ReAct evidence and code-claim gates
```

---

### Task 6: 全量回归（P1 验收）

- [ ] **Step 1: framework agent + 被引用包**

Run: `cd framework && go test ./agent ./tool ./model ./events ./mea -count=1`

Expected: PASS

- [ ] **Step 2: portal**

Run: `cd portal && go test ./internal/chat/... ./internal/service/... -count=1`

Expected: PASS

- [ ] **Step 3: 对照规格验收清单**

- `ReActConfig` 无 `EvidenceGate` / `CodeClaimGate`
- `PostModelPolicy` 仍在；`turn_intent_gate.go` 仍编译
- `evidence_tools.go` 仍在
- `plan_agent.go` 仍在
- `ShouldApplyEvidenceGate` 仍在
- 无 `[code_workset]` 注入
- **未**删除 investigation / task_lock / turn_surface

- [ ] **Step 4: 若有未提交改动则收尾 commit**

---

## 明确不做（P1）

- 不删 `PostModelPolicy` / TurnIntentGate / HTTP grounding / task lock / turn tool surface
- 不删 `mea_*` / growth / hub / hypertool
- 不实现 PromptBuilder、不改 workspace 默认根（P2）
- 不实现子 Agent
- 不搬家 `framework/harness` 包名
