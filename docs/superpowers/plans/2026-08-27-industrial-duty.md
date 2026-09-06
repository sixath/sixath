# 切片 D：值班底座 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把现网写拒绝、RCA 结案门、盖章观测钉进 `TestEvalGolden_deny_write` 与 `TestEvalGolden_close_gate`（及 chat/obs 两条），`scripts/industrial-eval.ps1` 能红。

**Architecture:** 不另起闸。SQL 写继续 `ExecuteOptions.AllowsWrite`；HITL 继续 `execute_write` 无 token 只提议；结案继续 `EvaluateEvidenceGate` + `ShouldApplyEvidenceGate` / `EvidenceGateTurnOption`（**不改** `chat.go` 第三参）。obs 唯一最低接线：`StampHitContract` → `obs.LogHitContract`。`executor.opRecorder.finish` **不改**（`Result` 无 `HitStatus`，禁止用 rows==0 猜 empty）。

**Tech Stack:** Go。根 `go.mod` 是空 module：portal 测 `cd portal`；executor/agent/tool/tool/data 测 `cd framework`。`framework/tool` 可 import `obs`（`mcp.go` 已如此）；禁止 `obs` import `tool` 或 `agent`。

**Spec:** `docs/superpowers/specs/2026-08-27-industrial-duty-design.md`  
**评测网:** `docs/superpowers/specs/2026-08-25-industrial-eval-design.md`

**不做:** 结案必须 `hit_status=hits`；改 `HasSuccessfulBoundEvidence`；`SATH_ALLOW_WRITE` 卸工具；把 `write_file` 改成全局拒绝；默全开 MEA；E0–E5；改声称闸读 pin；改正 Skill 索引；另起 RCA 词表；用 `HasPriorAssistant` 挡第一轮调查题；改 `EvidenceGateTurnOption` 入参为 `lock.Q`；给 `Result` 加 `HitStatus`；Prometheus 加 index/repo 标签；live LLM；新建平行评测框架；自动 git commit（除非用户另行要求）。

**夹具常量（写死）：**

```go
const meaESGoal = "用 elasticsearch 查一下错误日志" // portal 已有
```

`bf26Q` 已在 `portal/internal/chat` 同包。结案闸正文用现网 `evidence_gate_test.go` 的 `"root cause is OOM in svc-a"`。

---

## File Structure

| 文件 | 责任 |
|------|------|
| `framework/executor/evalgolden_test.go` | `TestEvalGolden_deny_write`（零值 INSERT 拒；AllowWrite 对照仍成功） |
| `framework/tool/data/evalgolden_test.go` | `TestEvalGolden_deny_write_pending`（无 token，Writer.Exec=0） |
| `portal/internal/chat/evalgolden_test.go` | `TestEvalGolden_close_gate_chat`；`TestEvalGolden_deny_write_files`（flags 默认 false） |
| `framework/agent/evalgolden_test.go` | `TestEvalGolden_close_gate` |
| `framework/obs/hit_contract.go` | `LogHitContract` + 测试钩子 |
| `framework/tool/evidence.go` | `HitStamp.Tool`/`Ctx`；盖章后调 `LogHitContract` |
| `framework/tool/es_log_tool.go`、`framework/tool/rca_code_tools.go` | 盖章处填 `Tool`/`Ctx` |
| `framework/tool/evalgolden_test.go` | `TestEvalGolden_obs_hit` |
| `scripts/industrial-eval.ps1` | 加 `./executor` |
| `docs/superpowers/specs/2026-08-25-industrial-eval-design.md` | §7 表加四行 |
| `docs/superpowers/specs/2026-08-27-industrial-duty-design.md` | 状态改为已确认；下一份指本 plan |

**不要改：** `portal/internal/service/chat.go`（第三参保持 `userForIntent`）、`HasSuccessfulBoundEvidence`、`EvaluateEvidenceGate` 的 `defaultRequireAnyOf`、`framework/executor/observe.go`、`framework/executor/executor.go` 的 `AllowsWrite` 默认、MEA 进门。

现网写/结案函数已存在。Task 1–4 的金样例**第一次跑就该绿**（钉子，不是先红后绿）。若零值放行写或无 ref 直接 Allow，必须红。Task 5 的 `obs_hit` 在接线前会 FAIL。

---

### Task 1: `TestEvalGolden_deny_write`

**Files:**
- Create: `framework/executor/evalgolden_test.go`
- 同包可复用 `mysql_test.go` 的 `dsWithDB`、`NewMySQLExecutor`

- [ ] **Step 1: 写金样例**

```go
package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sixath/framework/datasource"
)

func TestEvalGolden_deny_write(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	reg := datasource.NewRegistry()
	reg.RegisterType("mysql", func(cfg datasource.Config) (datasource.DataSource, error) {
		return &dsWithDB{id: cfg.ID, db: db}, nil
	})
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: "mysql"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ex := NewMySQLExecutor(reg)

	_, err = ex.Execute(context.Background(), "ds1", "INSERT INTO t (a) VALUES (1)", ExecuteOptions{})
	if !errors.Is(err, ErrReadOnlyViolation) {
		t.Fatalf("zero-value must deny write, got %v", err)
	}

	mock.ExpectExec("INSERT").WillReturnResult(sqlmock.NewResult(1, 1))
	_, err = ex.Execute(context.Background(), "ds1", "INSERT INTO t (a) VALUES (1)", ExecuteOptions{AllowWrite: true})
	if err != nil {
		t.Fatalf("AllowWrite opt-in must still work: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
```

零值分支**不要** `ExpectExec`（被拒则驱动不应收到写）。

- [ ] **Step 2: 跑测试**

Run: `cd E:\workspace\github\sixath\sixath\framework; go test ./executor -count=1 -run TestEvalGolden_deny_write`  
Expected: PASS

破坏（不要提交）：把 `AllowsWrite` 零值改成 `return true` → 本测试 FAIL。

---

### Task 2: HITL pending + 文件写默认关

**Files:**
- Modify: `framework/tool/data/evalgolden_test.go`（已有 `package tooldata`）
- Modify: `portal/internal/chat/evalgolden_test.go`

同包已有：`fakeWriteExecutor`、`fakeTokenGen`、`memoryPendingStore`、`fakeChecker`（`execute_write_test.go`）。

- [ ] **Step 1: pending 金样例**

在 `evalgolden_test.go` 追加（需要 `context`）：

```go
func TestEvalGolden_deny_write_pending(t *testing.T) {
	store := newMemoryPendingStore()
	ex := &fakeWriteExecutor{ret: &executor.Result{AffectedRows: 1}}
	cfg := &ExecuteWriteConfig{
		Writer:              ex,
		Exec:                ex,
		Checker:             &fakeChecker{allow: true},
		PendingStore:        store,
		TokenGen:            &fakeTokenGen{next: "t-deny"},
		DefaultDatasourceID: "ds1",
	}
	reg := core.NewRegistry()
	if err := RegisterExecuteWriteTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("execute_write")
	out, err := tl.Execute(context.Background(), map[string]any{
		"dsl":        "UPDATE t SET a=1",
		"session_id": "s1",
		"user_id":    "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, ok := out.(ExecuteWritePendingResponse)
	if !ok || resp.Status != "pending" || resp.Token == "" {
		t.Fatalf("%T %#v", out, out)
	}
	if len(ex.calls) != 0 {
		t.Fatalf("propose must not Exec, calls=%d", len(ex.calls))
	}
}
```

- [ ] **Step 2: 文件 flags**

在 `portal/internal/chat/evalgolden_test.go` 追加：

```go
func TestEvalGolden_deny_write_files(t *testing.T) {
	if DefaultHermesP0ToolFlags.WorkspaceFilesEnabled {
		t.Fatal("workspace files must default off (E5 opt-in)")
	}
	var zero HermesP0ToolFlags
	if zero.WorkspaceFilesEnabled {
		t.Fatal("HermesP0ToolFlags zero value must deny write_file")
	}
}
```

- [ ] **Step 3: 跑测试**

Run:

```
cd E:\workspace\github\sixath\sixath\framework; go test ./tool/data -count=1 -run TestEvalGolden_deny_write_pending
cd E:\workspace\github\sixath\sixath\portal; go test ./internal/chat -count=1 -run TestEvalGolden_deny_write_files
```

Expected: 两次 PASS

---

### Task 3: `TestEvalGolden_close_gate`

**Files:**
- Modify: `framework/agent/evalgolden_test.go`

现网 `EvaluateEvidenceGate` **不要改**。`defaultRequireAnyOf` 保持 `jaeger_trace`、`es_log_query`。

- [ ] **Step 1: 写金样例**

```go
func TestEvalGolden_close_gate(t *testing.T) {
	cfg := EvidenceGateConfig{Enabled: true}
	got := EvaluateEvidenceGate(cfg, nil, "root cause is OOM in svc-a")
	if got.Allow || got.Action != "inject" {
		t.Fatalf("no refs must inject: %#v", got)
	}

	got = EvaluateEvidenceGate(cfg, []tool.EvidenceRef{{Kind: "es_log_query", Summary: "no hits"}}, "root cause is OOM")
	if !got.Allow {
		t.Fatalf("empty ES ref still closes: %#v", got)
	}

	got = EvaluateEvidenceGate(cfg, []tool.EvidenceRef{{Kind: "jaeger_trace", TraceID: "abc"}}, "ok")
	if !got.Allow {
		t.Fatalf("jaeger ref: %#v", got)
	}

	got = EvaluateEvidenceGate(cfg, []tool.EvidenceRef{{Kind: "rca_grep", Path: "main.go"}}, "found a line")
	if got.Allow || got.Action != "inject" {
		t.Fatalf("grep alone must inject: %#v", got)
	}

	got = EvaluateEvidenceGate(cfg, nil, "本次无法定位，证据不足。")
	if !got.Allow {
		t.Fatalf("exemption: %#v", got)
	}
}
```

- [ ] **Step 2: 跑测试**

Run: `cd E:\workspace\github\sixath\sixath\framework; go test ./agent -count=1 -run TestEvalGolden_close_gate`  
Expected: PASS（`tool` 已在该文件 import）

破坏：无 ref 时 `EvaluateEvidenceGate` 直接 `Allow: true` → FAIL。不要把空击 ES 改成必须 `hits` 才 Allow。

---

### Task 4: `TestEvalGolden_close_gate_chat`

**Files:**
- Modify: `portal/internal/chat/evalgolden_test.go`
- **不要改** `portal/internal/service/chat.go`

同包已有 `builderGateFake`、`BuildReActAgent`、`EvidenceGateTurnOption`、`meaESGoal`、`bf26Q`。需要 `"github.com/sixath/framework/tool"`（若尚未 import 则加）。

- [ ] **Step 1: 写金样例**

```go
func TestEvalGolden_close_gate_chat(t *testing.T) {
	if ShouldApplyEvidenceGate(nil, "你好") {
		t.Fatal("hello")
	}
	if ShouldApplyEvidenceGate(nil, "有哪些技能") {
		t.Fatal("skills")
	}
	if ShouldApplyEvidenceGate(nil, bf26Q) {
		t.Fatal("bf26Q must not open close-gate (no RCA keywords)")
	}
	if !ShouldApplyEvidenceGate(nil, meaESGoal) {
		t.Fatal("es goal")
	}
	if len(AutoChecks("你好")) != 0 {
		t.Fatal("share C chat skip; do not fork keyword table")
	}

	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name: "es_log_query", Description: "es",
		Parameters: map[string]any{"type": "object"},
		Execute:    func(context.Context, map[string]any) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	if !ShouldEnableEvidenceGate(reg) {
		t.Fatal("es bound")
	}

	hello := BuildReActAgent(&builderGateFake{finalReply: "hi"}, reg, "", 10,
		agent.WithReActMaxSteps(2),
		EvidenceGateTurnOption(reg, nil, "你好"),
	).(*agent.ReActAgent)
	if hello.EvidenceGateEnabled() {
		t.Fatal("hello must disable EvidenceGate")
	}

	es := BuildReActAgent(&builderGateFake{finalReply: "root cause"}, reg, "", 10,
		agent.WithReActMaxSteps(2),
		EvidenceGateTurnOption(reg, nil, meaESGoal),
	).(*agent.ReActAgent)
	if !es.EvidenceGateEnabled() {
		t.Fatal("investigation must keep EvidenceGate")
	}
}
```

禁止引入 `HasPriorAssistant`。禁止把 `EvidenceGateTurnOption` 的 userText 换成 `lock.Q`。

- [ ] **Step 2: 跑测试**

Run: `cd E:\workspace\github\sixath\sixath\portal; go test ./internal/chat -count=1 -run "TestEvalGolden_close_gate_chat|TestEvalGolden_mea_chat_skip"`  
Expected: PASS

---

### Task 5: `LogHitContract` + `obs_hit`

**Files:**
- Create: `framework/obs/hit_contract.go`
- Modify: `framework/tool/evidence.go`（import `context`、`obs`）
- Modify: `framework/tool/es_log_tool.go`、`framework/tool/rca_code_tools.go`（填 `Tool`/`Ctx`）
- Modify: `framework/tool/evalgolden_test.go`

接线锁死：**`StampHitContract` 直接调 `obs.LogHitContract`**。不要函数变量 + `obs.HookHitContract` 双路径。`obs` 不得 import `tool`。因此 `TestEvalGolden_obs_hit` 必须放在 `package tool`（已有 `evalgolden_test.go`）。

**不要改** `observe.go`。不要给 `executor.Result` 加字段。`execute_read` 补日志是加分，**跳过**。

- [ ] **Step 1: 先写会失败的 `obs_hit`**

在 `framework/tool/evalgolden_test.go` 追加 import `"github.com/sixath/framework/obs"`，以及：

```go
func TestEvalGolden_obs_hit(t *testing.T) {
	var got []obs.HitContractLog
	restore := obs.SetHitContractHook(func(rec obs.HitContractLog) {
		got = append(got, rec)
	})
	defer restore()

	_ = StampHitContract(map[string]any{"hits": []any{}}, HitStamp{
		Status:       HitStatusEmpty,
		QueriedIndex: "vm-manager-*",
		Tool:         "es_log_query",
	})
	if len(got) != 1 {
		t.Fatalf("StampHitContract must LogHitContract, got %#v", got)
	}
	if got[0].Status != HitStatusEmpty || got[0].Index != "vm-manager-*" || got[0].Tool != "es_log_query" {
		t.Fatalf("%#v", got[0])
	}
}
```

- [ ] **Step 2: 确认失败**

Run: `cd E:\workspace\github\sixath\sixath\framework; go test ./tool -count=1 -run TestEvalGolden_obs_hit`  
Expected: FAIL（`obs.HitContractLog` / `SetHitContractHook` 未定义，或 `StampHitContract` 未调用）

- [ ] **Step 3: 实现 `obs.LogHitContract`**

`framework/obs/hit_contract.go`：

```go
package obs

import (
	"context"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type HitContractLog struct {
	Tool   string
	Status string
	Index  string
	Repo   string
}

var (
	hitHookMu sync.Mutex
	hitHook   func(HitContractLog)
)

func SetHitContractHook(fn func(HitContractLog)) func() {
	hitHookMu.Lock()
	prev := hitHook
	hitHook = fn
	hitHookMu.Unlock()
	return func() {
		hitHookMu.Lock()
		hitHook = prev
		hitHookMu.Unlock()
	}
}

func LogHitContract(ctx context.Context, toolName, status, queriedIndex, repo string) {
	if ctx == nil {
		ctx = context.Background()
	}
	rec := HitContractLog{Tool: toolName, Status: status, Index: queriedIndex, Repo: repo}
	hitHookMu.Lock()
	hook := hitHook
	hitHookMu.Unlock()
	if hook != nil {
		hook(rec)
	}
	attrs := []any{"tool", toolName}
	if status != "" {
		attrs = append(attrs, "hit_status", status)
	}
	if queriedIndex != "" {
		attrs = append(attrs, "queried_index", queriedIndex)
	}
	if repo != "" {
		attrs = append(attrs, "repo", repo)
	}
	slog.InfoContext(ctx, "hit_contract", attrs...)
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		var kv []attribute.KeyValue
		if toolName != "" {
			kv = append(kv, attribute.String("sixath.tool", toolName))
		}
		if status != "" {
			kv = append(kv, attribute.String("sixath.hit_status", status))
		}
		if queriedIndex != "" {
			kv = append(kv, attribute.String("sixath.queried_index", queriedIndex))
		}
		if repo != "" {
			kv = append(kv, attribute.String("sixath.repo", repo))
		}
		if len(kv) > 0 {
			span.SetAttributes(kv...)
		}
	}
}
```

otel 已是 framework 依赖。Prometheus **不要**加这些键当 label。

- [ ] **Step 4: `StampHitContract` 接线**

`HitStamp` 追加（JSON 盖章字段不变，只给 obs 用）：

```go
type HitStamp struct {
	Status       string
	QueriedIndex string
	Repo         string
	SetRepo      bool
	Tool         string          // obs
	Ctx          context.Context // nil → Background
}
```

`StampHitContract` 在写完 payload 之后、`return` 之前：

```go
if s.Status != "" {
	ctx := s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	obs.LogHitContract(ctx, s.Tool, s.Status, s.QueriedIndex, s.Repo)
}
```

`evidence.go` 增加 `context` 与 `"github.com/sixath/framework/obs"`。

生产盖章处补 `Tool`/`Ctx`（漏了金样例仍过，但日志没有 tool 名；**要补**）：

- `es_log_tool.go` 每一处 `HitStamp{` 加 `Tool: toolName, Ctx: ctx`
- `rca_code_tools.go` 的 grep 成功/错误盖章：`Tool: toolName, Ctx: ctx`；`stampRCAGrepErr` 增加 `ctx` 参数并传入，或在 helper 里 `Tool: "rca_grep"`

不要改盖章的 `hit_status` / `queried_index` / `repo` 语义。

- [ ] **Step 5: 跑测试**

Run:

```
cd E:\workspace\github\sixath\sixath\framework; go test ./tool -count=1 -run "TestEvalGolden_obs_hit|TestEvalGolden_empty_hit_stamp"
cd E:\workspace\github\sixath\sixath\framework; go test ./obs -count=1
```

Expected: PASS。故意删掉 `StampHitContract` 里的 `LogHitContract` → `TestEvalGolden_obs_hit` FAIL。

---

### Task 6: A 脚本 + 文档

**Files:**
- Modify: `scripts/industrial-eval.ps1`
- Modify: `docs/superpowers/specs/2026-08-25-industrial-eval-design.md`
- Modify: `docs/superpowers/specs/2026-08-27-industrial-duty-design.md`

- [ ] **Step 1: 脚本在 `./agent` 之前插入 `./executor`（fail-fast）**

只**插入**两行，不要删现有的 `./tool` / `./tool/data` / `./mea`。`./tool` 已覆盖 `obs_hit`，**不要**再加 `./obs`。

现网顺序应变为：portal chat → tool → tool/data → **executor** → agent → mea。

```powershell
go test ./executor -count=1 -run TestEvalGolden_ -v
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
```

插在 `go test ./agent ...` 那一段之前。

- [ ] **Step 2: eval spec §7 表追加（不要删 C/B 行）**

| ID | 锁什么 | 何时 |
|----|--------|------|
| `deny_write` | 零值 INSERT 拒写；pending 不 Exec；files 默认关 | **D** |
| `close_gate` | 调查题无 jaeger/es ref → inject；空击 ES 过闸 | **D** |
| `close_gate_chat` | 你好 / bf26Q 不开结案门 | **D** |
| `obs_hit` | StampHitContract 打出 hit_status + queried_index | **D** |

D spec 文首：`状态: 已确认（2026-08-27）`；`下一份` 改为 `docs/superpowers/plans/2026-08-27-industrial-duty.md`。

- [ ] **Step 3: 全脚本**

Run: `powershell -NoProfile -File E:\workspace\github\sixath\sixath\scripts\industrial-eval.ps1`  
Expected: 全部 `TestEvalGolden_` PASS（含 `./executor`）。`c7aa` 仍 Skip。

破坏：`AllowsWrite` 零值放行 → `TestEvalGolden_deny_write` FAIL。无 ref 结案 Allow → `TestEvalGolden_close_gate` FAIL。

---

## 验收对照（规格 §7）

| ID | 任务 |
|----|------|
| `deny_write` | Task 1 |
| `deny_write` opt-in 对照 | Task 1 第二段 |
| `deny_write_pending` | Task 2 |
| `deny_write` 文件 | Task 2 |
| `close_gate`（inject / 空击 / grep / 豁免） | Task 3 |
| `close_gate_chat` | Task 4 |
| `obs_hit` | Task 5 |
| A 脚本 | Task 6 |
