# Harness Phase 1：证据面 + 成长焊点 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 Phase 1：RCA 工具结果契约（E2/E3）、Final-answer EvidenceGate（E1）、Growth 挂到 `on_chat_session_end`（G2）、fork-agent cron 反写（G3）；验收 E5 绑定；可选可配置 nudge（G1）。

**Architecture:** 证据面与工具管线分离——RCA 工具 Execute 归一 `ok`/`error_code`/`evidence_refs`，ReAct 在「本步无 tool_calls」与 `forceFinalSummary` 前调用 Final-answer Evaluator（默认 Soft）。成长面把现有 `TrySessionEnd*` 从「每轮 assistant 后」改为（或兼挂）`ChatSessionHooks`，fork 复盘结束后用改名映射调用已有 `RewriteCronSkillRefs`。

**Tech Stack:** Go；`framework/agent`、`framework/tool`（RCA）；`portal/internal/service` Growth；既有 `biz.CronRefRewriteUsecase`。

**Spec:** `docs/superpowers/specs/2026-07-11-harness-engineering-gap-design.md` §3.3、§4.3–4.4、§5 Phase 1  
**前置:** Phase 0 已落地（ToolHook、HookBlocked、ChatSessionHookRegistry、`ChatService.ChatSessionHooks()`）

> **Git：** 无仓库则跳过 Commit。  
> **范围说明：** E5 以验收既有 `rca_builder` 为主，不重写 binding design。E4/G4/C4/CDP 不在本计划。G1 为可选末任务。

---

## 子系统拆分（可并行）

| Track | Tasks | 依赖 |
|-------|-------|------|
| **A 证据** | 1→2→3→4 | E2 必须先于 E1 |
| **B 成长** | 5→6 | 仅依赖 Phase 0 C2 |
| **C 验收** | 7（E5） | 可与 A/B 并行 |
| **D 可选** | 8（G1） | 建议 A/B 完成后 |

推荐执行顺序：1 → 2 → 3 → 4，并行穿插 5→6 与 7；最后可选 8。

---

## 文件结构

| 文件 | 职责 |
|------|------|
| Create `framework/tool/evidence.go` | `EvidenceRef`、从 RCA 结果 map 抽取 refs、`error_code` 常量 |
| Create `framework/tool/evidence_test.go` | 抽取/归一单测 |
| Modify `framework/tool/jaeger_tool.go` | 成功/失败结果带 `ok`/`error_code`/`evidence_refs` |
| Modify `framework/tool/es_log_tool.go` | 同上 |
| Modify `framework/tool/rca_code_tools.go` | grep/glob/read 同上（refs 含 `repo:path:line`） |
| Create `framework/agent/evidence_gate.go` | `EvidenceGateConfig`、`EvidenceGateEvaluator`、Soft/Hard、证据不足文案检测 |
| Create `framework/agent/evidence_gate_test.go` | 表驱动：缺证据 Soft；含「证据不足」放行；HardHalt |
| Modify `framework/agent/react_agent.go` | 配置 + final-answer / forceFinal 前调用 gate；累积 refs 自 Trace |
| Modify `framework/agent/trace.go` | 可选 `EvidenceRefs []EvidenceRef` 聚合字段 |
| Modify `framework/events/event.go` | `EvidenceIncomplete Kind`（可选但推荐） |
| Modify `portal/internal/service/growth_chat.go` / `chat.go` 构造 | G2：Register session-end hook；厘清与 `notifyGrowthAssistantTurn` 关系 |
| Modify `portal/internal/service/growth_agent_review.go` | G3：复盘后 cron 反写 |
| Modify `framework/docs/...` / gap spec | Phase 1 状态 |
| E5 | 对照 `docs/superpowers/specs/2026-07-08-rca-agent-binding-design.md` + 现有 `rca_builder*` 写验收清单测试 |

---

### Task 1: Evidence 类型与抽取（E2 基础）

**Files:**
- Create: `framework/tool/evidence.go`
- Create: `framework/tool/evidence_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestNormalizeEvidenceResult_okWithRefs(t *testing.T) {
	in := map[string]any{
		"trace_id": "abc",
		"spans":    []any{},
	}
	out := NormalizeRCAResult(in, EvidenceMeta{Tool: "jaeger_trace", OK: true})
	if out["ok"] != true {
		t.Fatalf("%#v", out)
	}
	refs, _ := out["evidence_refs"].([]EvidenceRef)
	if len(refs) == 0 || refs[0].Kind != "jaeger_trace" {
		t.Fatalf("refs=%#v", refs)
	}
}

func TestNormalizeEvidenceResult_transientError(t *testing.T) {
	out := NormalizeRCAResult(map[string]any{"error": "timeout"}, EvidenceMeta{Tool: "es_log_query", OK: false, ErrorCode: ErrorTransient})
	if out["ok"] != false || out["error_code"] != ErrorTransient {
		t.Fatalf("%#v", out)
	}
}

func TestCollectEvidenceRefsFromToolResults(t *testing.T) {
	// 从若干 tool result map 合并 refs
}
```

- [ ] **Step 2:** `cd framework && go test ./tool/ -run 'TestNormalizeEvidence|TestCollectEvidence' -v` → FAIL

- [ ] **Step 3: 实现**

```go
package tool

const (
	ErrorTransient = "transient"
	ErrorPermanent = "permanent"
)

type EvidenceRef struct {
	Kind    string `json:"kind"` // jaeger_trace | es_log_query | rca_grep | ...
	TraceID string `json:"trace_id,omitempty"`
	Repo    string `json:"repo,omitempty"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type EvidenceMeta struct {
	Tool      string
	OK        bool
	ErrorCode string // ErrorTransient | ErrorPermanent | ""
	Refs      []EvidenceRef // 若空则按 Tool 从 payload 推导最小 ref
}

// NormalizeRCAResult 合并业务 payload 与契约字段；不删除原字段。
func NormalizeRCAResult(payload map[string]any, meta EvidenceMeta) map[string]any { /* ... */ }

func CollectEvidenceRefs(results ...any) []EvidenceRef { /* 读 evidence_refs */ }
```

推导规则（写死）：
- `jaeger_trace` 成功：至少 1 条 `{kind, trace_id}`（从 payload `trace_id` 或 data）
- `es_log_query` 成功：`{kind:es_log_query, trace_id?}`；有 hits 即可
- `rca_*`：命中则带 `repo/path/line`

- [ ] **Step 4:** 测试 PASS

- [ ] **Step 5:** Commit（若有 git）`feat(tool): add RCA evidence_refs normalization helpers`

---

### Task 2: RCA 五工具接入契约（E2 + E3）

**Files:**
- Modify: `framework/tool/jaeger_tool.go`
- Modify: `framework/tool/es_log_tool.go`
- Modify: `framework/tool/rca_code_tools.go`
- Modify: 对应 `*_test.go`

**E3 映射（写死）：**
| 情况 | error_code |
|------|------------|
| 网络超时、5xx、executor 暂态 | `transient` |
| 缺参、越权 path、4xx 客户端、decode 永久失败 | `permanent` |
| 业务空结果但仍成功查询 | `ok=true`，refs 可空或带「空结果」summary |

- [ ] **Step 1:** 扩展既有 jaeger/es/rca 测试：成功响应含 `ok==true` 与 `evidence_refs`；模拟 timeout → `error_code=transient`

- [ ] **Step 2:** 跑测 FAIL（缺字段）

- [ ] **Step 3:** 在各 Execute 返回前包一层 `NormalizeRCAResult`；`jaegerGET` 错误区分：若 `err` 含 timeout/connection → transient，HTTP 4xx → permanent

- [ ] **Step 4:** `go test ./tool/ -run 'Jaeger|ESLog|RCA' -count=1` PASS

- [ ] **Step 5:** Commit `feat(tool): normalize RCA tool results with ok/error_code/evidence_refs`

---

### Task 3: EvidenceGate Evaluator（E1 纯逻辑）

**Files:**
- Create: `framework/agent/evidence_gate.go`
- Create: `framework/agent/evidence_gate_test.go`
- Modify: `framework/events/event.go` — 加 `EvidenceIncomplete Kind = "agent.evidence.incomplete"`

**默认策略（写死）：**
- 启用条件：`EvidenceGateConfig.Enabled`（或检测到 registry 含 `jaeger_trace`/`es_log_query` 且 `AutoEnableIfRCATools`）
- 要求：至少 1 条 kind∈`{jaeger_trace, es_log_query}` 的成功 ref（可配 `RequireAnyOf`）
- Soft：返回 `ActionInject` + 回压文案；Hard：`ActionHalt`
- 若最终答复文本匹配「证据不足」类短语（中英简单 contains），**放行**

```go
type EvidenceGateConfig struct {
	Enabled            bool
	HardHalt           bool
	RequireAnyOf       []string // default: jaeger_trace, es_log_query
	InsufficientOKText []string // default: "证据不足", "insufficient evidence"
}

type EvidenceGateResult struct {
	Allow  bool
	Action string // "" | "inject" | "halt"
	Reason string
	Prompt string // Soft 注入的 user/system 内容
}

func EvaluateEvidenceGate(cfg EvidenceGateConfig, refs []tool.EvidenceRef, finalText string) EvidenceGateResult
```

- [ ] **Step 1–4:** TDD 表驱动：无 refs + Soft → inject；无 refs + 文案含证据不足 → allow；无 refs + Hard → halt；有 jaeger ref → allow

- [ ] **Step 5:** Commit `feat(agent): add EvidenceGateEvaluator for RCA final-answer checks`

---

### Task 4: ReAct 接入 Final-answer Gate（E1）

**Files:**
- Modify: `framework/agent/react_agent.go`
- Modify: `framework/agent/react_agent_test.go`
- Modify: `framework/agent/trace.go`（`EvidenceNudges int`；可选聚合 `EvidenceRefs`）
- Modify: `portal/internal/chat/agent_builder.go`（或构造 ReAct 处）— **必须**在本 Task 完成产品启用

**接线点与 Soft 策略（写死，勿再二选一）：**

| 路径 | Soft 行为 |
|------|-----------|
| 循环内 `!stepInfo.Used` | 若不足且 `EvidenceNudges==0`：inject 回压 user 消息、`EvidenceNudges++`、`continue` 再给模型 **1** 轮；若已 nudge 仍不足：放行终稿并 `Metadata["evidence_incomplete"]=true` + emit `EvidenceIncomplete` |
| `forceFinalSummary` / Stream 强制结案 | **不再加步、不 `continue`**。仅评估：不足则 Soft 打 `Metadata["evidence_incomplete"]=true` + emit；HardHalt 则 `RunError`。强制结案路径**禁止**为 Soft 突破 `MaxSteps` |

**产品启用（本 Task 必做，勿推到 Task 7）：**
在 Portal `BuildReActAgent` / `agent_builder`（实际组装 `NewReActAgent` 处）检测 registry 是否含 `jaeger_trace` 或 `es_log_query`（或 agent 绑定了 RCA 工具），若是则：

```go
opts = append(opts, agent.WithReActEvidenceGate(agent.EvidenceGateConfig{
  Enabled: true,
  // HardHalt: false 默认 Soft
}))
```

集成测：构造含 `jaeger_trace` 的 registry + fake 模型直接终答 → 第一次循环 Soft inject 或 Metadata 标记；第二条测「证据不足」文案放行。

`WithReActEvidenceGate`；framework 单测可用手动 Enabled；**默认 Enabled=false** 保证非 RCA Agent 不变。

- [ ] **Step 1:** 写失败集成测（循环 Soft + forceFinal 仅 Metadata + Portal/builder 启用测）
- [ ] **Step 2:** FAIL
- [ ] **Step 3:** 实现 react + builder 启用
- [ ] **Step 4:** `go test ./agent/ ./...` 相关 + portal chat builder 测 PASS
- [ ] **Step 5:** Commit `feat(agent): wire EvidenceGate; enable for RCA-bound agents`

---

### Task 5: Growth → ChatSessionHooks（G2）— Spec 对齐方案 A

**Files:**
- Modify: `portal/internal/service/chat.go` / `growth_chat.go`
- Modify: `portal/internal/biz/growth.go`（`TrySessionEnd*` 调用点迁移）
- Test: `portal/internal/service/growth_session_hook_test.go`、更新 `biz/growth_test.go`

**方案 A（写死，满足 Spec G2）：**
1. **assistant 落库路径**（`notifyGrowthAssistantTurn`）：**只保留** `OnAssistantTurn` / 阈值计数（及既有 tool success 计数）。**移除**对 `TrySessionEndMemoryReview` / `TrySessionEndSkillReview` 的调用（不再在每轮 AgentRun 末「假 session-end」置 pending）。
2. **`ChatSessionHooks`**：在 `NewChatService` 注册 growth hook → `OnChatSessionClosed(ctx, sessionID)`，内部调用原 `TrySessionEndMemoryReview` + `TrySessionEndSkillReview`（置 pending + 唤醒 worker）。这样 Curator/脏标记/learnings 消费链仍走既有 Worker，但触发点真正挂在 ChatSession 结束。
3. **DeleteSession 顺序**：保持「delete 成功后再 hook」。因 `TrySessionEnd*` 主要读 growth 计数/pending 而非全量 transcript，删消息后仍可工作；若实现发现必须读消息，改为 hook-before-delete 并更新测。

**DoD：**
- DeleteSession → 触发 `TrySessionEnd*`（经 hook）
- 单轮 assistant 落库 → **不再**单独调用 `TrySessionEnd*`
- 既有 `growth_test` 中「session end」语义改为测 `OnChatSessionClosed` / DeleteSession 路径

- [ ] **Step 1:** 改测：assistant 路径不置 pending_skill/memory；DeleteSession 后置 pending（flag 开启时）
- [ ] **Step 2:** FAIL
- [ ] **Step 3:** 迁移调用 + Register hook
- [ ] **Step 4:** portal service/biz 测 PASS
- [ ] **Step 5:** Commit `feat(portal): move growth session-end review to ChatSessionHooks (G2)`

---

### Task 6: Fork-agent cron 技能反写（G3）

**Files:**
- Create: `framework/growth/skill_rename_diff.go` + `_test.go`（1:1 启发式）
- Modify: `portal/internal/service/growth_agent_review.go`
- Modify: `portal/internal/service/growth_worker.go` — **注入并保存** `cronRewrite`：把已有 `RunnerDeps.RewriteCronSkillRefs`（或 `CronRefRewriteUsecase.RewriteForWorkspace`）赋到 `GrowthWorker` 字段，供 `spawnReviewAgent` 调用（当前只有 RunnerDeps，没有 `w.cronRewrite`）
- Test: wiring 测

**做法：**
1. 复盘前：`skills.Index` name 集合 `before`
2. 成功后：`after`；仅 `len(removed)==1 && len(added)==1` → `old→new`
3. `w.cronRewrite(ctx, workspace, renames)`；多对多 Skip+Warn
4. 删除「phase-1 gap」Warn

- [ ] **Step 1–5:** TDD + Commit `feat(growth): rewrite cron refs after fork-agent 1:1 skill rename`

---

### Task 7: E5 RCA 绑定验收（不重做 design）

**Files:**
- 对照：`docs/superpowers/specs/2026-07-08-rca-agent-binding-design.md`
- 已有：`portal/internal/chat/rca_builder.go`、`agent_builder.go`、`biz/tool.go`
- Create: `portal/internal/chat/rca_binding_acceptance_test.go`（或扩展现有）

**验收清单（全部断言）：**
- [ ] `ToolTypeRCA` 合法
- [ ] `func_path` ∈ `rca_code|jaeger_trace|es_log_query`
- [ ] `BuildRegistry` 注册后 `ListForAPI` 可见对应工具名
- [ ] 文档勾选：与 binding design 一致；缺口记入 gap spec「E5 已验收」

若发现缺失（如 web 表单），单开 follow-up issue，**不**在本计划实现前端。

- [ ] **Step 1–3:** 补验收测试 → PASS  
- [ ] **Step 4:** 更新 gap spec E5 行状态  
- [ ] **Step 5:** Commit `test(portal): RCA agent binding acceptance checks for Phase 1 E5`

---

### Task 8（可选）: 可配置 Growth nudge（G1）

**Files:** 视现有 Growth 计数而定（`OnToolSuccess` / `OnAssistantTurn`）

- Conf 开关：`growth.nudge_enabled`（默认 false 或保持现有 assistant 路径行为）
- 文档说明与 Hermes nudge 的差异：Sixath 以 Worker + session hooks 为主，主循环 nudge 为增强

若时间不够：**跳过本 Task**，在 gap spec 标 G1 deferred。

---

### Task 9: 文档与全量回归

- [ ] 更新 `docs/superpowers/specs/2026-07-11-harness-engineering-gap-design.md`：Phase 1 已落地条目  
- [ ] `framework/docs/design-agent-runtime-hermes-inspired.md` 增一小节 EvidenceGate（Final-answer）  
- [ ] 运行：
```bash
cd framework && go test ./agent/ ./tool/ ./growth/ ./events/ -count=1
cd portal && go test ./internal/service/ ./internal/chat/ ./internal/biz/ -count=1
```
- [ ] Commit docs

---

## 完成定义（对照 spec Phase 1）

| ID | 验收 |
|----|------|
| E2 | RCA 五工具成功/失败 JSON 含 `ok`；成功含 `evidence_refs`；失败含 `error_code` |
| E3 | 超时类 → `transient`；越权/缺参 → `permanent` |
| E1 | RCA 启用 gate：循环内 Soft nudge ≤1；forceFinal 不足只 Metadata+事件；「证据不足」放行；非 RCA 默认关闭 |
| E5 | 验收测试绿；绑定 design 无未解释缺口 |
| G2 | DeleteSession → `TrySessionEnd*`；assistant 落库路径**不再**调用 `TrySessionEnd*` |
| G3 | 1:1 技能改名后 fork 路径调用 cron rewrite；`GrowthWorker` 持有 rewrite 回调 |

---

## 风险

| 风险 | 缓解 |
|------|------|
| Soft nudge 死循环 | 循环内上限 1 次 inject；forceFinal **不加步** |
| 误伤非 RCA Agent | 默认 Enabled=false；**builder 仅 RCA 工具启用** |
| G2 行为变化 | assistant 不再假 session-end；需回归 Growth pending 仅在删会话时出现 |
| G3 启发式误改名 | 仅 1:1；否则 skip |
| G3 未注入 | Task 6 强制 `GrowthWorker.cronRewrite` 字段 |
