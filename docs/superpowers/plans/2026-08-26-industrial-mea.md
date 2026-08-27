# 切片 C：MEA 产品化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Agent 已开 MEA 且任务可机检时，无需手写 `mea-checks` 也能进外环；`completed` 只来自审计；`TestEvalGolden_mea_no_fence` 与 `TestEvalGolden_mea_claim` 在 A 脚本里能红。

**Architecture:** 入口用 `ShouldUseMEA` + `AutoChecks(lock.Q)` 放下事后契约（不读 `hit_status`）。Executor 把本轮 `RunTrace` + 终答拷进 `ExecutionReport`。RulesAuditor 新增 `trace_hit_status` / `empty_hit_speak`，调用现网 `EvaluateEmptyHitSpeakGate`。`ApplyAudit` 不改。

**Tech Stack:** Go。根 `go.mod` 是空 module：portal 测 `cd portal`；mea/agent 测 `cd framework`。`framework/mea` 可 import `framework/agent`；禁止反向。

**Spec:** `docs/superpowers/specs/2026-08-26-industrial-mea-design.md`  
**评测网:** `docs/superpowers/specs/2026-08-25-industrial-eval-design.md`

**不做:** 默全开 MEA；每步 LLM 裁判；D/E；改声称闸读 pin；改正 Skill 索引；为救 `bf26Q` 另起关键词表；live LLM；新建平行评测框架；自动 git commit（除非用户另行要求）。

**夹具常量（写死，与 `TestShouldApplyEvidenceGate` 对齐）：**

```go
const meaESGoal = "用 elasticsearch 查一下错误日志"
```

`bf26Q` 已在 `portal/internal/chat/task_lock_test.go`（同包可直接用）。

---

## File Structure

| 文件 | 责任 |
|------|------|
| `framework/mea/types.go` | `ToolHit`；`ExecutionReport.FinalText` / `ToolHits`；`AcceptanceCheck.Type` 注释补两 type |
| `framework/mea/rules_auditor.go` | trace 类跳过 `resolvePath`；两 type 读报告 |
| `framework/mea/evalgolden_test.go` | `TestEvalGolden_mea_claim`、`TestEvalGolden_mea_empty_speak` |
| `portal/internal/chat/mea_autochecks.go` | `AutoChecks`、`ShouldUseMEA`、`ResolveAcceptanceChecks`、`MEAAcceptancePrompt` |
| `portal/internal/chat/mea_report.go` | `ToolHitsFromTrace`、`LastAssistantText` |
| `portal/internal/chat/evalgolden_test.go` | `TestEvalGolden_mea_no_fence`、`TestEvalGolden_mea_chat_skip`；C3–C7 可同文件非金样例名 |
| `portal/internal/service/chat.go` | `lock` 之后 `ResolveAcceptanceChecks` + `ShouldUseMEA` |
| `portal/internal/service/mea_stream.go` | `streamEpisode`；填 `FinalText`/`ToolHits`；用 `MEAAcceptancePrompt` |
| `scripts/industrial-eval.ps1` | 加 `go test ./mea -run TestEvalGolden_` |
| `docs/superpowers/specs/2026-08-25-industrial-eval-design.md` | §7 表加四行 |
| `docs/superpowers/specs/2026-08-26-industrial-mea-design.md` | 状态改为已确认；下一份指本 plan |

`apply.go` **不要改**。三个证据工具盖章 **不要改**。

---

### Task 1: ExecutionReport 字段

**Files:**
- Modify: `framework/mea/types.go`

- [ ] **Step 1: 追加类型**

在 `AcceptanceCheck` 注释改为：

```go
Type string `json:"type"` // path_exists | file_contains | json_path | trace_hit_status | empty_hit_speak
```

在 `ExecutionReport` **现有字段后面**追加（不要删 `ClaimComplete`）：

```go
type ToolHit struct {
	ToolName     string `json:"tool_name"`
	HitStatus    string `json:"hit_status,omitempty"`
	QueriedIndex string `json:"queried_index,omitempty"`
	Repo         string `json:"repo,omitempty"`
	Error        string `json:"error,omitempty"`
	Blocked      bool   `json:"blocked,omitempty"`
}
```

```go
type ExecutionReport struct {
	Round            int      `json:"round"`
	Summary          string   `json:"summary"`
	ArtifactsTouched []string `json:"artifacts_touched,omitempty"`
	Issues           []string `json:"issues,omitempty"`
	ClaimComplete    bool     `json:"claim_complete,omitempty"`
	FinalText        string   `json:"final_text,omitempty"`
	ToolHits         []ToolHit `json:"tool_hits,omitempty"`
}
```

- [ ] **Step 2: 编译**

Run: `cd E:\workspace\github\sixath\sixath\framework; go test ./mea -count=1`  
Expected: PASS（只加字段，现有测试仍过）

---

### Task 2: AutoChecks + 金样例 no_fence / chat_skip

**Files:**
- Create: `portal/internal/chat/mea_autochecks.go`
- Modify: `portal/internal/chat/evalgolden_test.go`

- [ ] **Step 1: 写失败测试（先于实现）**

在 `evalgolden_test.go` 追加：

```go
const meaESGoal = "用 elasticsearch 查一下错误日志"

func TestEvalGolden_mea_no_fence(t *testing.T) {
	got := AutoChecks(meaESGoal)
	if len(got) != 2 || got[0].Type != "trace_hit_status" || got[1].Type != "empty_hit_speak" {
		t.Fatalf("%#v", got)
	}
}

func TestEvalGolden_mea_chat_skip(t *testing.T) {
	for _, s := range []string{"你好", "有哪些技能", "继续", bf26Q} {
		if got := AutoChecks(s); len(got) != 0 {
			t.Fatalf("%q → %#v", s, got)
		}
	}
}

func TestAutoChecks_deliveryUsesQ(t *testing.T) {
	hist := []model.Message{
		{Role: "user", Content: meaESGoal},
		{Role: "assistant", Content: "正在查"},
		{Role: "user", Content: "没有打印出来呀"},
	}
	lock := BuildTurnTaskLock("没有打印出来呀", hist)
	if lock.Q != meaESGoal {
		t.Fatalf("Q=%q", lock.Q)
	}
	if len(AutoChecks(lock.Q)) != 2 {
		t.Fatal("AutoChecks(G) must be non-empty")
	}
	if len(AutoChecks(lock.Delivery)) != 0 {
		t.Fatalf("AutoChecks(D) must be empty, D=%q", lock.Delivery)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd E:\workspace\github\sixath\sixath\portal; go test ./internal/chat -count=1 -run "TestEvalGolden_mea_no_fence|TestEvalGolden_mea_chat_skip|TestAutoChecks_deliveryUsesQ"`  
Expected: FAIL（`AutoChecks` 未定义）

- [ ] **Step 3: 最小实现**

`mea_autochecks.go`（`package chat`）：

```go
package chat

import (
	"log"
	"strings"

	"github.com/sixath/framework/mea"
)

func AutoChecks(goal string) (out []mea.AcceptanceCheck) {
	defer func() {
		if rec := recover(); rec != nil {
			out = nil
			log.Printf("mea_autochecks=error recovered=%v", rec)
		}
	}()
	goal = strings.TrimSpace(goal)
	if goal == "" || !ShouldApplyEvidenceGate(nil, goal) {
		return nil
	}
	return []mea.AcceptanceCheck{
		{Type: "trace_hit_status"},
		{Type: "empty_hit_speak"},
	}
}

func ResolveAcceptanceChecks(fence []mea.AcceptanceCheck, fenceOK bool, goal string) []mea.AcceptanceCheck {
	if fenceOK {
		return fence
	}
	return AutoChecks(goal)
}

func traceOnlyChecks(checks []mea.AcceptanceCheck) bool {
	if len(checks) == 0 {
		return false
	}
	for _, c := range checks {
		switch c.Type {
		case "trace_hit_status", "empty_hit_speak":
		default:
			return false
		}
	}
	return true
}

func ShouldUseMEA(enabled bool, workspace string, checks []mea.AcceptanceCheck, acceptance []string) bool {
	if !enabled {
		return false
	}
	if len(checks) == 0 && len(acceptance) == 0 {
		return false
	}
	if strings.TrimSpace(workspace) != "" {
		return true
	}
	return traceOnlyChecks(checks) && len(acceptance) == 0
}
```

禁止另起关键词表；必须调用 `ShouldApplyEvidenceGate(nil, …)`。`active != nil` 那条看的是本轮已激活族，**不要**传非 nil 的 active。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd E:\workspace\github\sixath\sixath\portal; go test ./internal/chat -count=1 -run "TestEvalGolden_mea_|TestAutoChecks_deliveryUsesQ|TestShouldApplyEvidenceGate"`  
Expected: PASS

---

### Task 3: ShouldUseMEA 进门谓词（C4–C7）

**Files:**
- Modify: `portal/internal/chat/evalgolden_test.go`（或同包 `mea_autochecks_test.go`，不要新建 `evalgolden/` 包）

- [ ] **Step 1: 写测试**

```go
func TestShouldUseMEA_predicates(t *testing.T) {
	es := AutoChecks(meaESGoal)
	if !ShouldUseMEA(true, "", es, nil) {
		t.Fatal("C5 traceOnly + empty workspace")
	}
	if ShouldUseMEA(false, "/ws", es, nil) {
		t.Fatal("disabled must not enter")
	}
	if ShouldUseMEA(true, "/ws", nil, nil) {
		t.Fatal("C4 no checks no acceptance")
	}
	file := []mea.AcceptanceCheck{{Type: "path_exists", Path: "out.txt"}}
	if ShouldUseMEA(true, "", file, nil) {
		t.Fatal("C6 file checks need workspace")
	}
	if !ShouldUseMEA(true, "/ws", file, nil) {
		t.Fatal("file checks + workspace")
	}
	got := ResolveAcceptanceChecks(file, true, meaESGoal)
	if len(got) != 1 || got[0].Type != "path_exists" {
		t.Fatalf("C7 %#v", got)
	}
	got = ResolveAcceptanceChecks(nil, false, meaESGoal)
	if len(got) != 2 {
		t.Fatalf("no fence → AutoChecks %#v", got)
	}
	if !ShouldUseMEA(true, "/ws", AutoChecks("你好"), []string{"done"}) {
		t.Fatal("acceptance-only + workspace still enters")
	}
}
```

- [ ] **Step 2: 跑测试**

Run: `cd E:\workspace\github\sixath\sixath\portal; go test ./internal/chat -count=1 -run TestShouldUseMEA_predicates`  
Expected: PASS（Task 2 已实现谓词）。若 C5 失败，检查 `traceOnlyChecks` 是否要求 `len(acceptance)==0`（在 `ShouldUseMEA` 里，不在 `traceOnlyChecks`）。

---

### Task 4: RulesAuditor 两 type + `TestEvalGolden_mea_empty_speak`

**Files:**
- Modify: `framework/mea/rules_auditor.go`
- Create: `framework/mea/evalgolden_test.go`

现网 `Audit` **对每条 check 先 `resolvePath`**。Path 为空会 `empty path` → `IntegrityViolation`。trace 类必须在 `resolvePath` **之前**分支。

- [ ] **Step 1: 写失败测试**

`evalgolden_test.go`（`package mea`）：

```go
package mea

import (
	"context"
	"testing"
)

func autoTraceChecks() []AcceptanceCheck {
	return []AcceptanceCheck{{Type: "trace_hit_status"}, {Type: "empty_hit_speak"}}
}

func esEmptyHit() ToolHit {
	return ToolHit{
		ToolName:     "es_log_query",
		HitStatus:    "empty",
		QueriedIndex: "vm-manager-*",
	}
}

func TestEvalGolden_mea_empty_speak(t *testing.T) {
	aud := RulesAuditor{WorkDir: ""}
	c := Contract{Round: 1, TargetRecordID: "r1", AcceptanceChecks: autoTraceChecks()}

	t.Run("deny", func(t *testing.T) {
		v, err := aud.Audit(context.Background(), TaskState{}, c, ExecutionReport{
			ClaimComplete: true,
			FinalText:     "该服务从未参与",
			ToolHits:      []ToolHit{esEmptyHit()},
		})
		if err != nil {
			t.Fatal(err)
		}
		if v.Completion != CompletionIncomplete {
			t.Fatalf("T-speak %+v", v)
		}
		if len(v.ProposedUpdates) != 0 {
			t.Fatalf("must not propose completed: %+v", v.ProposedUpdates)
		}
	})

	t.Run("ok", func(t *testing.T) {
		v, err := aud.Audit(context.Background(), TaskState{}, c, ExecutionReport{
			FinalText: "该索引 0 条，不能据此说从未参与，查了 vm-manager-*",
			ToolHits:  []ToolHit{esEmptyHit()},
		})
		if err != nil {
			t.Fatal(err)
		}
		if v.Completion != CompletionComplete || v.Integrity != IntegrityClean {
			t.Fatalf("T-speak-ok %+v", v)
		}
		if len(v.ProposedUpdates) != 1 || v.ProposedUpdates[0].Status != StatusCompleted {
			t.Fatalf("proposed %+v", v.ProposedUpdates)
		}
	})

	t.Run("missing_hit_status", func(t *testing.T) {
		v, err := aud.Audit(context.Background(), TaskState{}, c, ExecutionReport{
			FinalText: "ok",
			ToolHits:  []ToolHit{{ToolName: "es_log_query"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if v.Completion != CompletionIncomplete {
			t.Fatalf("T-hit %+v", v)
		}
	})
}
```

保留 `framework/mea/rules_auditor_test.go` 里文件类测试；改完后必须仍绿。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd E:\workspace\github\sixath\sixath\framework; go test ./mea -count=1 -run TestEvalGolden_mea_empty_speak`  
Expected: FAIL（`empty path` 或 unknown type）

- [ ] **Step 3: 最小实现**

在 `rules_auditor.go` 增加 import：`"github.com/sixath/framework/agent"`。

`Audit` 循环内，`resolvePath` 之前：

```go
if check.Type == "trace_hit_status" || check.Type == "empty_hit_speak" {
	ok, excerpt, checkErr := a.runTraceCheck(check, o)
	// 与文件类相同的 ok/checkErr 记账（incomplete+suspect，continue）
	// 成功则 Evidence Excerpt "pass"
	continue
}
```

把 `Audit` 的第四参从 `_ ExecutionReport` 改成 `o ExecutionReport`。

```go
func (a RulesAuditor) runTraceCheck(check AcceptanceCheck, o ExecutionReport) (ok bool, excerpt string, err error) {
	switch check.Type {
	case "trace_hit_status":
		for _, h := range o.ToolHits {
			if h.Error != "" || h.Blocked {
				continue
			}
			switch h.ToolName {
			case "es_log_query", "execute_read", "rca_grep":
			default:
				continue
			}
			switch h.HitStatus {
			case "hits", "empty", "error":
				return true, "pass", nil
			}
		}
		return false, "no stamped es_log_query/execute_read/rca_grep hit_status", nil
	case "empty_hit_speak":
		got := agent.EvaluateEmptyHitSpeakGate(runTraceFromHits(o.ToolHits), o.FinalText)
		if !got.Allow {
			ex := got.Reason
			if got.Prompt != "" {
				ex = got.Prompt
			}
			return false, ex, nil
		}
		return true, "pass", nil
	default:
		return false, fmt.Sprintf("unknown check type %q", check.Type), nil
	}
}

func runTraceFromHits(hits []ToolHit) *agent.RunTrace {
	tr := &agent.RunTrace{}
	for _, h := range hits {
		res := map[string]any{}
		if h.HitStatus != "" {
			res["hit_status"] = h.HitStatus
		}
		if h.QueriedIndex != "" {
			res["queried_index"] = h.QueriedIndex
		}
		if h.Repo != "" {
			res["repo"] = h.Repo
		}
		tr.ToolCalls = append(tr.ToolCalls, agent.ToolCallRecord{
			ToolName: h.ToolName,
			Result:   res,
			Error:    h.Error,
			Blocked:  h.Blocked,
		})
	}
	return tr
}
```

文件类 `runCheck` 的 `default` 仍返回 unknown type。不要让 trace 类走进 `resolvePath`。空 `WorkDir` 只跑 trace 不得 panic。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd E:\workspace\github\sixath\sixath\framework; go test ./mea -count=1`  
Expected: PASS（含原 `TestRulesAuditor_*`）

---

### Task 5: `TestEvalGolden_mea_claim`（钉 ApplyAudit）

**Files:**
- Modify: `framework/mea/evalgolden_test.go`
- 不要改 `apply.go`

- [ ] **Step 1: 写测试（现网已满足，应立刻绿）**

```go
func TestEvalGolden_mea_claim(t *testing.T) {
	s := TaskState{Records: []TaskRecord{{ID: "r1", Kind: KindRequirement, Status: StatusPending}}}
	_ = ExecutionReport{ClaimComplete: true}
	out := ApplyAudit(s, AuditReport{
		ID:         "a1",
		Completion: CompletionIncomplete,
		Integrity:  IntegritySuspect,
		ProposedUpdates: []ProposedUpdate{{
			RecordID: "r1",
			Status:   StatusCompleted,
			Summary:  "executor claimed",
		}},
	})
	if out.Records[0].Status == StatusCompleted {
		t.Fatal("ClaimComplete/incomplete audit must not complete")
	}
	if out.Records[0].Status != StatusPending {
		t.Fatalf("status=%s", out.Records[0].Status)
	}
}
```

故意在 `ApplyAudit` 里让 incomplete 也写 `completed` → 本测试必须 FAIL。

- [ ] **Step 2: 跑测试**

Run: `cd E:\workspace\github\sixath\sixath\framework; go test ./mea -count=1 -run TestEvalGolden_mea_claim`  
Expected: PASS

---

### Task 6: 进门接线 + 验收提示

**Files:**
- Modify: `portal/internal/chat/mea_autochecks.go`（追加 `MEAAcceptancePrompt`）
- Modify: `portal/internal/service/chat.go`（约 896–901）
- Modify: `portal/internal/service/mea_stream.go`（`messagesForMEAContract`）
- Test: `portal/internal/chat/evalgolden_test.go` 或 `mea_autochecks_test.go`

- [ ] **Step 1: 提示纯函数测试**

```go
func TestMEAAcceptancePrompt_traceOnly(t *testing.T) {
	p := MEAAcceptancePrompt(AutoChecks(meaESGoal), nil)
	if strings.Contains(p, "produce environment state") {
		t.Fatal(p)
	}
	if !strings.Contains(p, "hit_status") {
		t.Fatal(p)
	}
}

func TestMEAAcceptancePrompt_fileKeepsEnv(t *testing.T) {
	p := MEAAcceptancePrompt([]mea.AcceptanceCheck{{Type: "path_exists", Path: "out.txt"}}, nil)
	if !strings.Contains(p, "produce environment state") {
		t.Fatal(p)
	}
}
```

- [ ] **Step 2: 实现 `MEAAcceptancePrompt`**

```go
func MEAAcceptancePrompt(checks []mea.AcceptanceCheck, acceptance []string) string {
	var b strings.Builder
	if traceOnlyChecks(checks) && len(acceptance) == 0 {
		b.WriteString("本轮用 ES/SQL/grep 调查。验收读工具 JSON 的 hit_status 与终答，不要为过检去 write_file。")
		return b.String()
	}
	if len(checks) > 0 {
		b.WriteString("[MEA acceptance — produce environment state that passes these checks]\n")
		for _, ck := range checks {
			fmt.Fprintf(&b, "- type=%s path=%s pattern=%s json_path=%s equals=%s\n",
				ck.Type, ck.Path, ck.Pattern, ck.JSONPath, ck.Equals)
		}
		return strings.TrimSuffix(b.String(), "\n")
	}
	if len(acceptance) > 0 {
		b.WriteString("[MEA acceptance — satisfy these observable criteria]\n")
		for _, line := range acceptance {
			fmt.Fprintf(&b, "- %s\n", line)
		}
		return strings.TrimSuffix(b.String(), "\n")
	}
	return ""
}
```

`messagesForMEAContract`：Goal 仍写在前；验收块改成 `chat.MEAAcceptancePrompt(c.AcceptanceChecks, c.Acceptance)`。有 prompt 才追加 `\n\n`。`mea_autochecks.go` 增加本函数时补 `fmt` import。

- [ ] **Step 3: 替换 `chat.go` 进门**

在已有 `lock := buildTurnTaskLockFromHistory` **之后**的 goroutine 里，替换：

```go
useMEA := (len(meaChecks) > 0 || len(meaAcceptance) > 0) &&
    chat.MEAEnabledForAgent(...) &&
    strings.TrimSpace(agentMeta.Workspace) != ""
```

为：

```go
enabled := chat.MEAEnabledForAgent(session.AgentID, agentMeta.RuntimeTools.MEAEnabled)
g := strings.TrimSpace(lock.Q)
if g == "" {
	g = userContent
}
checks := meaChecks
if enabled {
	checks = chat.ResolveAcceptanceChecks(meaChecks, len(meaChecks) > 0, g)
}
useMEA := chat.ShouldUseMEA(enabled, agentMeta.Workspace, checks, meaAcceptance)
```

`len(meaChecks) > 0` 当作 `fenceOK`（与 `ParseMEAChecks` 空/失败不写 meaChecks 一致）。有围栏 checks 时 `ResolveAcceptanceChecks` **不会**调用 `AutoChecks`。未 enabled 时不要调用 `AutoChecks`。

`streamWithRulesMEA` 完整改成（不要继续传 `userContent` / 空 `meaChecks`）：

```go
if useMEA {
	s.streamWithRulesMEA(runCtx, sessionID, session.AgentID, agentMeta.Workspace, g, checks, meaAcceptance, agentMeta.RuntimeTools.MEAEnabled, m, a, messages, req.Metadata, streamSessionProvider, ch)
	return
}
```

- [ ] **Step 4: 跑测试**

Run: `cd E:\workspace\github\sixath\sixath\portal; go test ./internal/chat -count=1 -run "TestEvalGolden_mea_|TestShouldUseMEA_|TestMEAAcceptancePrompt_|TestAutoChecks_"`  
Expected: PASS

`go test ./internal/service` 若因签名未改而红，等 Task 7。

---

### Task 7: ExecutionReport 填终答与 ToolHits

**Files:**
- Create: `portal/internal/chat/mea_report.go`
- Modify: `portal/internal/service/mea_stream.go`
- Modify: `portal/internal/service/chat.go`（`streamAgentEvents` 调用处）

- [ ] **Step 1: 转换函数测试**

`portal/internal/chat/mea_report.go` + 同包测试：

```go
func LastAssistantText(msgs []model.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && strings.TrimSpace(msgs[i].Content) != "" {
			return msgs[i].Content
		}
	}
	return ""
}

func ToolHitsFromTrace(tr *agent.RunTrace) []mea.ToolHit {
	if tr == nil {
		return nil
	}
	out := make([]mea.ToolHit, 0, len(tr.ToolCalls))
	for _, c := range tr.ToolCalls {
		st, idx, repo := tool.HitContractFromResult(c.Result)
		out = append(out, mea.ToolHit{
			ToolName:     c.ToolName,
			HitStatus:    st,
			QueriedIndex: idx,
			Repo:         repo,
			Error:        c.Error,
			Blocked:      c.Blocked,
		})
	}
	return out
}
```

测试：`LastAssistantText` 取最后一条非空 assistant；`ToolHitsFromTrace` 对 `hit_status=empty` + `queried_index=vm-manager-*` 的 `es_log_query` 能抽出字段。缺 `hit_status` 的 Result → `HitStatus==""`。

- [ ] **Step 2: `streamAgentEvents` 改为返回 episode**

同包未导出：

```go
type streamEpisode struct {
	Summary   string
	FinalText string
	Trace     *agent.RunTrace
}
```

签名改为 `(streamEpisode, error)`。

- 非流式 `a.Run`：`FinalText = strings.TrimSpace(resp.Text)`；`Trace = chat.RunTraceFromMetadata(resp.Metadata)`（已有 `tr`）。**不要**用 chunk 拼接当 FinalText。
- 流式：`StreamEventDone` 时 `ep.FinalText = chat.LastAssistantText(ev.Messages)`（即使 `ev.Trace == nil` 也要写终答，不要把这行放进 `if ev.Trace != nil`）。`ep.Trace = ev.Trace`。Delta **只**给 `Summary` 与 SSE chunk，**不**写入 `FinalText`。
- `chat.go` 纯 ReAct：`if _, err := s.streamAgentEvents(...)` 改成忽略 episode。

Executor 成功路径：

```go
ep, err := s.streamAgentEvents(...)
...
return mea.ExecutionReport{
	Round:         c.Round,
	Summary:       ep.Summary,
	ClaimComplete: true,
	FinalText:     ep.FinalText,
	ToolHits:      chat.ToolHitsFromTrace(ep.Trace),
}, nil
```

失败路径：同样带上已有的 `FinalText`/`ToolHits`（可能空），`ClaimComplete: false`。禁止再跑一轮 Agent，禁止从 SSE 字符串反解析工具 JSON。

- [ ] **Step 3: 编译 portal**

Run: `cd E:\workspace\github\sixath\sixath\portal; go test ./internal/chat ./internal/service -count=1 -run TestEvalGolden_mea_`  
Expected: chat 包 PASS；service 包若无该 run 则 `go test ./internal/service -count=1` 须编译过（现有测试绿）。

---

### Task 8: 评测脚本与文档

**Files:**
- Modify: `scripts/industrial-eval.ps1`
- Modify: `docs/superpowers/specs/2026-08-25-industrial-eval-design.md` §7 表
- Modify: `docs/superpowers/specs/2026-08-26-industrial-mea-design.md` 文首状态

- [ ] **Step 1: 脚本在 `go test ./agent` 之后插入 fail-fast，再跑 mea**

现网最后一条是 `./agent` 后直接 `exit $LASTEXITCODE`。插入 mea 后必须先拦住 agent 失败，否则 mea 绿会盖掉 agent 红。改成：

```powershell
go test ./agent -count=1 -run TestEvalGolden_ -v
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go test ./mea -count=1 -run TestEvalGolden_ -v
exit $LASTEXITCODE
```

`mea_autochecks.go` 增加 `MEAAcceptancePrompt` 时补 `fmt` import（Task 2 文件只有 `log`/`strings`/`mea`）。

- [ ] **Step 2: eval spec §7 表追加四行（不要删已有 `empty_hit` B 行）**

| ID | 锁什么 | 何时 |
|----|--------|------|
| `mea_no_fence` | 调查题无围栏 → AutoChecks 两条 | **C** |
| `mea_chat_skip` | 你好 / bf26Q → 空 checks | **C** |
| `mea_claim` | incomplete 审计不得 completed | **C** |
| `mea_empty_speak` | 空击「从未参与」→ incomplete | **C** |

C spec 文首：`状态: 已确认（2026-08-26）`；`下一份` 改为本 plan 路径。

- [ ] **Step 3: 全脚本**

Run: `powershell -NoProfile -File E:\workspace\github\sixath\sixath\scripts\industrial-eval.ps1`  
Expected: 全部 `TestEvalGolden_` PASS（含 `./mea`）。`c7aa` 仍 Skip。

破坏：`AutoChecks("你好")` 返回两条 → `TestEvalGolden_mea_chat_skip` FAIL。`ApplyAudit` 在 incomplete 时写下 completed → `TestEvalGolden_mea_claim` FAIL。

---

## 验收对照（规格 §6）

| # | 任务 |
|---|------|
| C1 / `mea_no_fence` | Task 2 |
| C2 / `mea_chat_skip` | Task 2 |
| C3 | Task 2 `TestAutoChecks_deliveryUsesQ` |
| C4–C7 | Task 3 |
| T-claim / `mea_claim` | Task 5 |
| T-speak / T-speak-ok / T-hit | Task 4 |
| 进门 + 提示 | Task 6 |
| 报告接线 | Task 7 |
| A 脚本 | Task 8 |
