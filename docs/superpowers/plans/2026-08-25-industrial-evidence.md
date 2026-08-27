# 切片 B：证据语义 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 三个证据工具盖上 `hit_status` + 实际 index/repo；终答把空击说成「从未参与」时 Retry；`TestEvalGolden_empty_hit` 在 A 脚本里能红。

**Architecture:** 工具出结果时 `StampHitContract`（禁止 agent 事后猜）。纯函数 `EvaluateEmptyHitSpeakGate` 挂在 `checkAnswerGates` 最前，默认开，不依赖 `CodeClaimGate.Enabled`。空击仍算 `HasSuccessfulBoundEvidence`。

**Tech Stack:** Go；根 `go.mod` 是空 module，必须 `cd framework`。规范化用 `strings.ToLower` + `strings.Fields`（夹具全是 BMP，不为此 `go get` `golang.org/x/text`）。

**Spec:** `docs/superpowers/specs/2026-08-25-industrial-evidence-design.md`  
**评测网:** `docs/superpowers/specs/2026-08-25-industrial-eval-design.md`

**不做:** live LLM；改声称闸读 pin；改 `HasSuccessfulBoundEvidence`；强制 `list_tables`；改正 Skill 索引；C/D/E；`inbound_empty`；自动 git commit（除非用户另行要求）。

---

## File Structure

| 文件 | 责任 |
|------|------|
| `framework/tool/evidence.go` | `HitStatus*` 常量、`StampHitContract`、`HitStatusFromCount`、`HitContractFromResult` |
| `framework/tool/es_log_tool.go` | 成功/rcaErr 写入 `hit_status`、`queried_index` |
| `framework/tool/rca_code_tools.go` | grep 成功/失败写入 `hit_status`、顶层 `repo` |
| `framework/executor/reader.go` | `QueryResult.HitStatus`、`QueriedIndex` json 字段 |
| `framework/tool/data/execute_read.go` | 成功返回前盖章；失败仍 `return nil, err` |
| `framework/agent/empty_hit_speak_gate.go` | `EvaluateEmptyHitSpeakGate` |
| `framework/agent/react_agent.go` | `checkAnswerGates` 最先跑空击闸；`EmptyHitNudges` |
| `framework/agent/trace.go` | `EmptyHitNudges int` |
| `framework/agent/evalgolden_test.go` | `TestEvalGolden_empty_hit*`（闸 T1–T7） |
| `framework/tool/evalgolden_test.go` | `TestEvalGolden_empty_hit_stamp`（G1–G3、G5） |
| `framework/tool/data/evalgolden_test.go` | `TestEvalGolden_empty_hit_stamp_read`（G4） |
| `scripts/industrial-eval.ps1` | 加 `./tool` 与 `./tool/data` |

`NormalizeRCAResult` **不要**自动给所有 RCA 工具盖章。

顶层 `repo`（grep，写死）：`params.repo` 非空用它，否则 `repoNameFromRoot(sel[0])`；无 sel 的 error 路径不要求顶层 repo。

---

### Task 1: 盖章 helper + 抽取（T8）

**Files:**
- Modify: `framework/tool/evidence.go`
- Modify: `framework/tool/evidence_test.go`

- [ ] **Step 1: 写失败测试**

在 `evidence_test.go` 追加：

```go
func TestStampHitContract_empty(t *testing.T) {
	out := StampHitContract(map[string]any{"hits": []any{}}, HitStamp{Status: HitStatusEmpty, QueriedIndex: "vm-manager-*"})
	if out["hit_status"] != HitStatusEmpty || out["queried_index"] != "vm-manager-*" {
		t.Fatalf("%#v", out)
	}
}

func TestHitContractFromResult_missingNotHits(t *testing.T) {
	st, _, _ := HitContractFromResult(map[string]any{"hits": []any{}})
	if st != "" {
		t.Fatalf("missing hit_status must not be hits, got %q", st)
	}
	st, idx, repo := HitContractFromResult(map[string]any{
		"hit_status": HitStatusEmpty, "queried_index": "vm-manager-*", "repo": "svc",
	})
	if st != HitStatusEmpty || idx != "vm-manager-*" || repo != "svc" {
		t.Fatalf("%q %q %q", st, idx, repo)
	}
}

func TestHitStatusFromCount(t *testing.T) {
	if HitStatusFromCount(true, 0) != HitStatusEmpty || HitStatusFromCount(true, 2) != HitStatusHits || HitStatusFromCount(false, 0) != HitStatusError {
		t.Fatal("HitStatusFromCount")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd E:\workspace\github\sixath\sixath\framework; go test ./tool -count=1 -run "TestStampHitContract_empty|TestHitContractFromResult_missingNotHits|TestHitStatusFromCount"`  
Expected: FAIL（未定义符号）

- [ ] **Step 3: 最小实现**

在 `evidence.go` 的 `ErrorPermanent` 常量旁追加：

```go
const (
	HitStatusHits  = "hits"
	HitStatusEmpty = "empty"
	HitStatusError = "error"
)

type HitStamp struct {
	Status       string
	QueriedIndex string
	Repo         string
	SetRepo      bool // true：即使 Repo=="" 也写 "repo" 键（grep 0 击）
}

func HitStatusFromCount(ok bool, n int) string {
	if !ok {
		return HitStatusError
	}
	if n <= 0 {
		return HitStatusEmpty
	}
	return HitStatusHits
}

func StampHitContract(payload map[string]any, s HitStamp) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	if s.Status != "" {
		payload["hit_status"] = s.Status
	}
	if s.QueriedIndex != "" {
		payload["queried_index"] = s.QueriedIndex
	}
	if s.SetRepo || s.Repo != "" {
		payload["repo"] = s.Repo
	}
	return payload
}

func HitContractFromResult(v any) (status, queriedIndex, repo string) {
	m, ok := v.(map[string]any)
	if !ok {
		return "", "", ""
	}
	return hitStatusString(m["hit_status"]), evidenceStringVal(m["queried_index"]), evidenceStringVal(m["repo"])
}

func hitStatusString(v any) string {
	s := strings.TrimSpace(evidenceStringVal(v))
	switch s {
	case HitStatusHits, HitStatusEmpty, HitStatusError:
		return s
	default:
		return ""
	}
}
```

`evidence.go` 本步 **不要** import `executor`。`HitContractFromResult` 只处理 `map[string]any` 和 default；QueryResult 分支在 Task 4 补。

- [ ] **Step 4: 跑测试确认通过**

Run: 同 Step 2  
Expected: PASS

---

### Task 2: es_log_query 盖章（G1–G3）

**Files:**
- Create: `framework/tool/evalgolden_test.go`
- Modify: `framework/tool/es_log_tool.go`

- [ ] **Step 1: 写失败测试**

`evalgolden_test.go`（`package tool`，复用同包 `fakeReader` / `errReader`）：

```go
package tool

import (
	"context"
	"errors"
	"testing"

	"github.com/sixath/framework/executor"
)

func TestEvalGolden_empty_hit_stamp(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{Columns: []string{"message"}, Rows: nil}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	if err := RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "app-logs-*", TraceIDField: "trace_id"}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{"query": "x", "index": "vm-manager-*"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["hit_status"] != HitStatusEmpty || m["queried_index"] != "vm-manager-*" {
		t.Fatalf("G1 %#v", m)
	}
	if m["ok"] != true {
		t.Fatalf("empty must remain ok=true")
	}

	fr.result = &executor.QueryResult{Columns: []string{"message"}, Rows: [][]any{{"a"}}}
	out, _ = tl.Execute(context.Background(), map[string]any{"query": "x", "index": "vm-manager-*"})
	if out.(map[string]any)["hit_status"] != HitStatusHits {
		t.Fatalf("G2 %#v", out)
	}

	reg2 := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg2, &errReader{err: errors.New("es down")}, ESLogConfig{DatasourceID: "es", DefaultIndex: "vm-manager-*", TraceIDField: "trace_id"})
	tl2, _ := reg2.Get("es_log_query")
	out, _ = tl2.Execute(context.Background(), map[string]any{"query": "x"})
	m = out.(map[string]any)
	if m["hit_status"] != HitStatusError {
		t.Fatalf("G3 %#v", m)
	}
	if m["queried_index"] != "vm-manager-*" {
		t.Fatalf("G3 index %#v", m)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd E:\workspace\github\sixath\sixath\framework; go test ./tool -count=1 -run TestEvalGolden_empty_hit_stamp`  
Expected: FAIL（无 `hit_status`）

- [ ] **Step 3: 盖章 es_log_query**

`Execute` 里在 `return rcaOK` / `rcaErr*` 之前盖章。`index` 变量已有。

成功路径（`rcaOK` 之前），条数**只**用这一套（与规格「hits 空且 total==0」一致）：

```go
n := len(rowsToHits(res))
if t := totalFromResult(res); t > n {
	n = t
}
payload = StampHitContract(payload, HitStamp{
	Status:       HitStatusFromCount(true, n),
	QueriedIndex: index,
})
return rcaOK(toolName, payload), nil
```

失败路径：`rcaErr` / `rcaErrFrom` 返回的已是 `map[string]any`，不要再 `.()`：

```go
out := rcaErrFrom(toolName, err)
return StampHitContract(out, HitStamp{Status: HitStatusError, QueriedIndex: index}), nil
```

现网 `either trace_id or query` 的 early return 在 `index := cfg.DefaultIndex` **之前**。盖章前把 index 计算上移到该 if 之上：

```go
index := cfg.DefaultIndex
if v, _ := params["index"].(string); strings.TrimSpace(v) != "" {
	index = v
}
if strings.TrimSpace(traceID) == "" && strings.TrimSpace(query) == "" {
	return StampHitContract(rcaErr(toolName, "either trace_id or query is required", ErrorPermanent), HitStamp{
		Status: HitStatusError, QueriedIndex: index,
	}), nil
}
```

后面原来的 `index := cfg.DefaultIndex` 块删掉，避免重复声明。

- [ ] **Step 4: 跑测试确认通过**

Run: 同 Step 2  
Expected: PASS

再跑：`go test ./tool -count=1 -run TestESLogQuery_`  
Expected: PASS（旧测试不得红）

---

### Task 3: rca_grep 盖章（G5）

**Files:**
- Modify: `framework/tool/evalgolden_test.go`
- Modify: `framework/tool/rca_code_tools.go`

- [ ] **Step 1: 写失败测试**

追加到 `evalgolden_test.go`（可与 G1 同函数末尾，或新 `TestEvalGolden_empty_hit_stamp_grep`，前缀必须 `TestEvalGolden_`）：

```go
func TestEvalGolden_empty_hit_stamp_grep(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	writeFile(t, filepath.Join(repoA, "a.go"), "package a\n")
	reg := newRCARegistry(t, []string{repoA})
	tl, _ := reg.Get("rca_grep")
	out, err := tl.Execute(context.Background(), map[string]any{"pattern": "NoSuchTokenXYZ"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["hit_status"] != HitStatusEmpty {
		t.Fatalf("G5 status %#v", m)
	}
	if _, ok := m["repo"]; !ok {
		t.Fatal("G5 missing top-level repo")
	}
	if m["repo"] != "service-a" {
		t.Fatalf("G5 repo=%v", m["repo"])
	}
	if m["ok"] != true {
		t.Fatal("empty grep must stay ok")
	}
}
```

需要 `path/filepath`。`writeFile` / `newRCARegistry` 已在 `rca_code_tools_test.go` 同包。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd E:\workspace\github\sixath\sixath\framework; go test ./tool -count=1 -run TestEvalGolden_empty_hit_stamp_grep`  
Expected: FAIL

- [ ] **Step 3: grep 成功返回盖章**

`registerRCAGrepTool` 成功 `rcaOK` 前：

```go
topRepo := strings.TrimSpace(repo)
if topRepo == "" && len(sel) > 0 {
	topRepo = repoNameFromRoot(sel[0])
}
payload := map[string]any{"matches": matches, "truncated": truncated}
payload = StampHitContract(payload, HitStamp{
	Status:  HitStatusFromCount(true, len(matches)),
	Repo:    topRepo,
	SetRepo: true,
})
return rcaOK(toolName, payload), nil
```

error 路径（`rcaErr` / `rcaErrFrom`）盖 `HitStatusError`；有 `repo` 参数则带上。不必为 pattern 缺失写顶层 repo。

- [ ] **Step 4: 跑测试确认通过**

Run: 同 Step 2；另 `go test ./tool -count=1 -run TestRCAGrep_EmptyMatchesOK`  
Expected: PASS

---

### Task 4: execute_read 盖章（G4）

**Files:**
- Modify: `framework/executor/reader.go`
- Modify: `framework/tool/data/execute_read.go`
- Modify: `framework/tool/evidence.go`（`HitContractFromResult` 补 QueryResult）
- Create: `framework/tool/data/evalgolden_test.go`

- [ ] **Step 1: 给 QueryResult 加字段**

```go
type QueryResult struct {
	Columns        []string
	Rows           [][]any
	Truncated      bool
	EstimatedTotal int64
	HitStatus      string `json:"hit_status,omitempty"`
	QueriedIndex   string `json:"queried_index,omitempty"`
}
```

`queryResultFromResult` / `resultFromQueryResult` **不要**丢这两字段（若 `Result` 没有同名字段，转换时保持零值即可，盖章发生在 `execute_read` 返回前）。

- [ ] **Step 2: 写失败测试**

`framework/tool/data/evalgolden_test.go`：

```go
package tooldata

import (
	"context"
	"testing"

	"github.com/sixath/framework/executor"
	core "github.com/sixath/framework/tool"
)

func TestEvalGolden_empty_hit_stamp_read(t *testing.T) {
	f := &fakeExecutor{ret: &executor.Result{Columns: []string{"id"}, Rows: nil}}
	cfg := &ExecuteReadConfig{Reader: f, Exec: f, DefaultDatasourceID: "ds1"}
	reg := core.NewRegistry()
	if err := RegisterExecuteReadTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("execute_read")
	out, err := tl.Execute(context.Background(), map[string]any{"dsl": "SELECT 1 WHERE 0"})
	if err != nil {
		t.Fatal(err)
	}
	res, ok := out.(*executor.QueryResult)
	if !ok {
		t.Fatalf("%T", out)
	}
	if res.HitStatus != core.HitStatusEmpty {
		t.Fatalf("G4 status %q", res.HitStatus)
	}
	if res.QueriedIndex != "" {
		t.Fatalf("G4 no index param, got %q", res.QueriedIndex)
	}
}
```

- [ ] **Step 3: 跑测试确认失败**

Run: `cd E:\workspace\github\sixath\sixath\framework; go test ./tool/data -count=1 -run TestEvalGolden_empty_hit_stamp_read`  
Expected: FAIL（HitStatus 空）

- [ ] **Step 4: 返回前盖章**

`execute_read.go` 在 `return res, nil` 前（`res` 非 nil）：

```go
n := 0
if res != nil {
	n = len(res.Rows)
	idx := ""
	if v, _ := params["index"].(string); strings.TrimSpace(v) != "" {
		idx = strings.TrimSpace(v)
	}
	res.HitStatus = tool.HitStatusFromCount(true, n)
	res.QueriedIndex = idx
}
return res, nil
```

需要 `strings`。失败路径不动。

`HitContractFromResult` 补 `*executor.QueryResult` / `executor.QueryResult`（import executor）。

- [ ] **Step 5: 跑测试确认通过**

Run: 同 Step 3；`go test ./tool/data -count=1 -run TestExecuteRead_`  
Expected: PASS

---

### Task 5: 终答闸纯函数（T1–T7）

**Files:**
- Create: `framework/agent/empty_hit_speak_gate.go`
- Modify: `framework/agent/evalgolden_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `evalgolden_test.go`：

```go
func emptyESTrace() *RunTrace {
	return &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query",
		Result:   map[string]any{"ok": true, "hit_status": "empty", "queried_index": "vm-manager-*"},
	}}}
}

func TestEvalGolden_empty_hit(t *testing.T) {
	tr := emptyESTrace()
	got := EvaluateEmptyHitSpeakGate(tr, "该服务从未参与")
	if got.Allow || got.Reason != "empty_hit_speak" {
		t.Fatalf("T1 %#v", got)
	}
	got = EvaluateEmptyHitSpeakGate(tr, "该索引 0 条，不能据此说从未参与，查了 vm-manager-*")
	if !got.Allow {
		t.Fatalf("T2 %#v", got)
	}
	got = EvaluateEmptyHitSpeakGate(tr, "Redis 里 key 不存在")
	if !got.Allow {
		t.Fatalf("T3 %#v", got)
	}
	got = EvaluateEmptyHitSpeakGate(&RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query",
		Result:   map[string]any{"hits": []any{}},
	}}}, "该服务从未参与")
	if !got.Allow {
		t.Fatalf("T7 fail-open %#v", got)
	}
}

func TestEvalGolden_empty_hit_unscoped(t *testing.T) {
	got := EvaluateEmptyHitSpeakGate(emptyESTrace(), "这条链路没有参与")
	if got.Allow {
		t.Fatalf("T4 %#v", got)
	}
}

func TestEvalGolden_empty_hit_scoped_ok(t *testing.T) {
	got := EvaluateEmptyHitSpeakGate(emptyESTrace(), "vm-manager-* 上没有匹配行")
	if !got.Allow {
		t.Fatalf("T5 %#v", got)
	}
}

func TestEvalGolden_empty_hit_grep_ignored(t *testing.T) {
	got := EvaluateEmptyHitSpeakGate(&RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "rca_grep",
		Result:   map[string]any{"hit_status": "empty", "repo": "service-a"},
	}}}, "该服务从未参与")
	if !got.Allow {
		t.Fatalf("T6 %#v", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd E:\workspace\github\sixath\sixath\framework; go test ./agent -count=1 -run TestEvalGolden_empty_hit`  
Expected: FAIL（函数不存在）

- [ ] **Step 3: 实现闸**

`empty_hit_speak_gate.go`：

```go
package agent

import (
	"os"
	"strings"
	"unicode"

	"github.com/sixath/framework/tool"
)

const (
	emptyHitSpeakReason = "empty_hit_speak"
	emptyHitGateEnv     = "SATH_EMPTY_HIT_GATE"
)

func emptyHitSpeakGateDisabled() bool {
	return strings.TrimSpace(os.Getenv(emptyHitGateEnv)) == "0"
}

func EvaluateEmptyHitSpeakGate(trace *RunTrace, finalText string) EvidenceGateResult {
	if emptyHitSpeakGateDisabled() {
		return EvidenceGateResult{Allow: true}
	}
	scopes, hasEmpty := collectEmptySpeakScopes(trace)
	if !hasEmpty {
		return EvidenceGateResult{Allow: true}
	}
	norm := normalizeEmptyHitText(finalText)
	work := stripEmptyHitAllowPhrases(norm)
	if containsAnySubstr(work, emptyHitDenyA) {
		return emptyHitReject(scopes)
	}
	scoped := textHasAnyScope(norm, scopes) // 范围看删除豁免前的原文，index 才还在
	skipBExist := redisKeyAbsence(norm)
	if !scoped {
		for _, p := range emptyHitDenyB {
			if skipBExist && (p == "不存在" || p == "does not exist") {
				continue
			}
			if strings.Contains(work, p) {
				return emptyHitReject(scopes)
			}
		}
	}
	return EvidenceGateResult{Allow: true}
}

func emptyHitReject(scopes []string) EvidenceGateResult {
	listed := strings.Join(scopes, ", ")
	if listed == "" {
		listed = "es_log_query/execute_read"
	}
	prompt := "空击（0 条）只能写未查到，不能写从未参与 / 服务不存在。本轮查询范围：" + listed +
		"。换 index 再查是建议，不是必须。弱命题须带上上述 index/repo。"
	return EvidenceGateResult{Allow: false, Action: "inject", Reason: emptyHitSpeakReason, Prompt: prompt}
}
```

其余辅助写在同文件（包内未导出即可）：

- `emptyHitDenyA` / `emptyHitDenyB`：规格 §4 原词，比较前已 lower，表内也用小写。
- `normalizeEmptyHitText`：`strings.ToLower` + `strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")`。
- `stripEmptyHitAllowPhrases`：依次删 `不能据此说从未参与`、`不能说从未参与`、`cannot conclude never`、`未查到`、`0 条`、`0 hits`、`没有匹配行`。
- `collectEmptySpeakScopes`：只认 `ToolName` 为 `es_log_query` 或 `execute_read`；`Error==""` && `!Blocked`；`HitContractFromResult` 的 status == `empty`。把非空 `queried_index`、`repo` 放进 scopes。`hasEmpty` 为是否至少一条 empty。
- `textHasAnyScope`：scopes 里任一非空串是 `norm` 的子串。
- `redisKeyAbsence`：`norm` 含 `redis` 且（`key` 或 `键`）且（`不存在` 或 `nil`）。
- `containsAnySubstr`

禁止 A 用 `work`（已删豁免）。禁止 B 的「有范围」用 **原文 `norm`**（T5 含 `没有匹配行` 会从 work 删掉，但 index 仍在 norm 里；T2 的 `vm-manager-*` 也在 norm）。

T2 正文含 `0 条` 和 `不能据此说从未参与`，删完后不应再含禁止 A。T5 删掉 `没有匹配行` 后若无禁止 B 即放行；即便残留「没有」，禁止 B 没有单独的「没有」。

- [ ] **Step 4: 跑测试确认通过**

Run: 同 Step 2  
Expected: PASS（含 unscoped / scoped_ok / grep_ignored，因为 `-run TestEvalGolden_empty_hit` 是前缀）

---

### Task 6: 接入 checkAnswerGates

**Files:**
- Modify: `framework/agent/trace.go`
- Modify: `framework/agent/react_agent.go`
- Modify: `framework/agent/evalgolden_test.go`（或同包新测试）

- [ ] **Step 1: 写失败测试**

```go
func TestEvalGolden_empty_hit_injects(t *testing.T) {
	a := &ReActAgent{}
	tr := emptyESTrace()
	got := a.checkAnswerGates(context.Background(), nil, nil, tr, "该服务从未参与", true, true, nil)
	if !got.Inject || got.Prompt == "" || !strings.Contains(got.Prompt, "vm-manager-*") {
		t.Fatalf("inject %#v", got)
	}
	if got.EmptyHit != true {
		t.Fatalf("EmptyHit flag %#v", got)
	}
	applyAnswerGateInject(tr, got)
	if tr.EmptyHitNudges != 1 {
		t.Fatalf("EmptyHitNudges=%d", tr.EmptyHitNudges)
	}

	got = a.checkAnswerGates(context.Background(), nil, nil, tr, "该服务从未参与", false, false, nil)
	if got.Inject || !got.Incomplete {
		t.Fatalf("forceFinal %#v", got)
	}
	found := false
	for _, e := range tr.Errors {
		if strings.Contains(e, "empty_hit_speak") {
			found = true
		}
	}
	if !found {
		t.Fatalf("trace.Errors=%v", tr.Errors)
	}

	t.Setenv("SATH_EMPTY_HIT_GATE", "0")
	got = a.checkAnswerGates(context.Background(), nil, nil, emptyESTrace(), "该服务从未参与", true, true, nil)
	if got.Inject {
		t.Fatal("env 0 must skip")
	}
}
```

`t.Setenv` 后不要污染后续测试：Go 1.17+ `t.Setenv` 会自动 restore。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd E:\workspace\github\sixath\sixath\framework; go test ./agent -count=1 -run TestEvalGolden_empty_hit_injects`  
Expected: FAIL

- [ ] **Step 3: 接线**

`trace.go` 的 `RunTrace` 在 `CodeClaimNudges` 旁：

```go
EmptyHitNudges int `json:"empty_hit_nudges,omitempty"`
```

`evidenceGateCheck` 增加 `EmptyHit bool`。

`applyAnswerGateInject`：**在现有 `CodeClaim` 分支之前追加** `EmptyHit`，不要整函数替换（否则证据闸 nudges 会坏）：

```go
if gate.EmptyHit {
	trace.EmptyHitNudges++
	return
}
if gate.CodeClaim {
	trace.CodeClaimNudges++
	return
}
trace.EvidenceNudges++
```

`checkAnswerGates` 开头：

```go
func (a *ReActAgent) checkAnswerGates(...) evidenceGateCheck {
	eh := a.checkEmptyHitSpeakGate(trace, finalText, allowInject, hasStepRoom, emit)
	if eh.HaltErr != nil || eh.Inject {
		return eh
	}
	ev := a.checkEvidenceGate(...)
	if ev.HaltErr != nil || ev.Inject {
		if eh.Incomplete {
			ev.Incomplete = true
		}
		return ev
	}
	cc := a.checkCodeClaimGate(...)
	if eh.Incomplete {
		cc.Incomplete = true
	}
	if ev.Incomplete {
		cc.Incomplete = true
	}
	return cc
}
```

`checkEmptyHitSpeakGate`：

```go
func (a *ReActAgent) checkEmptyHitSpeakGate(trace *RunTrace, finalText string, allowInject, hasStepRoom bool, emit func(events.Kind, map[string]any)) evidenceGateCheck {
	result := EvaluateEmptyHitSpeakGate(trace, finalText)
	if result.Allow {
		return evidenceGateCheck{}
	}
	if allowInject && result.Action == "inject" && trace != nil && trace.EmptyHitNudges == 0 && hasStepRoom {
		return evidenceGateCheck{Inject: true, EmptyHit: true, Prompt: result.Prompt}
	}
	if trace != nil {
		trace.Errors = append(trace.Errors, emptyHitSpeakReason)
	}
	if emit != nil {
		emit(events.EvidenceIncomplete, map[string]any{"reason": emptyHitSpeakReason})
	}
	return evidenceGateCheck{Incomplete: true, EmptyHit: true}
}
```

无独立 event kind 时复用 `EvidenceIncomplete` + `reason`（规格允许事件 payload 带 reason）。不要 halt。

- [ ] **Step 4: 跑测试确认通过**

Run: Step 2 命令；`go test ./agent -count=1 -run "TestEvalGolden_|TestReActEvidenceGate_|TestCredentialSolicitationRedirect_skipsWhenEvidenceExists"`  
Expected: PASS。确认空击 trace 仍让 e9d4 redirect 为 false（不改 `HasSuccessfulBoundEvidence`）。

---

### Task 7: A 脚本扩面

**Files:**
- Modify: `scripts/industrial-eval.ps1`

- [ ] **Step 1: 脚本增加两包**

`framework` 目录下现为只跑 `./agent`。改为：

```powershell
$ErrorActionPreference = "Stop"
$root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location (Join-Path $root "portal")
go test ./internal/chat -count=1 -run TestEvalGolden_ -v
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Set-Location (Join-Path $root "framework")
go test ./tool -count=1 -run TestEvalGolden_ -v
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go test ./tool/data -count=1 -run TestEvalGolden_ -v
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go test ./agent -count=1 -run TestEvalGolden_ -v
exit $LASTEXITCODE
```

- [ ] **Step 2: 跑全脚本**

Run: `powershell -NoProfile -File E:\workspace\github\sixath\sixath\scripts\industrial-eval.ps1`  
Expected: exit 0；输出含 `TestEvalGolden_empty_hit`、`TestEvalGolden_empty_hit_stamp`、`TestEvalGolden_empty_hit_stamp_read`、以及 A 原有 bf26/e9d4/c304/8555。

- [ ] **Step 3: 破坏必红（手改后还原）**

临时把 `HitStatusFromCount` 在 `n<=0` 时改成返回 `hits`，跑 `go test ./tool -count=1 -run TestEvalGolden_empty_hit_stamp` → FAIL。还原。

临时从 `emptyHitDenyA` 去掉 `从未参与`，跑 `go test ./agent -count=1 -run TestEvalGolden_empty_hit$` → T1 FAIL。还原。

---

## 验收对照规格

| 规格 | 任务 |
|------|------|
| G1–G3 | Task 2 |
| G5 | Task 3 |
| G4 | Task 4 |
| T1–T3、T7 | Task 5 `TestEvalGolden_empty_hit` |
| T4 | Task 5 `TestEvalGolden_empty_hit_unscoped` |
| T5–T6 | Task 5 |
| T8 | Task 1 |
| 闸挂点 / env / forceFinal | Task 6 |
| A 脚本 | Task 7 |
| 不改凭据成功定义 | Task 6 回归 e9d4 |
