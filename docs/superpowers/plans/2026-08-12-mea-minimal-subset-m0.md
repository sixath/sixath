# MEA Minimal Subset M0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.  
> Spec confirmed 2026-08-12 on branch `feature/mea-minimal-subset`.  
> **Do not commit unless the user asks.** Prefer one logical commit per Task when committing is requested.

**Goal:** Deliver M0 — session JSON TaskState, Contract, rules/script audit gate (executor cannot mark `completed`), feature flag + `data_root/mea` store — without LLM Auditor or UI.

**Architecture:** New `framework/mea` package owns types, atomic file store, `ApplyAudit`, `RulesAuditor`, and a small `Orchestrator` over Manager/Executor/Auditor interfaces. Portal wires `data_root`, `SATH_MEA` / pilot gate, and a thin entry that can run MEA around existing ReAct later; M0 chat path may use a **programmatic Manager** (bootstrap requirements from goal + structured acceptance) so tests and pilots work before M1 LLM Auditor.

**Tech Stack:** Go 1.26; `github.com/sixath/framework`; Portal `data.data_root`; env flags like Turn Tool Surface.

**Spec:** [docs/superpowers/specs/2026-08-12-mea-minimal-subset-design.md](../specs/2026-08-12-mea-minimal-subset-design.md) （M0 only）

---

## File map

| File | Responsibility |
|------|----------------|
| Create `framework/mea/types.go` | `TaskState`, `TaskRecord`, `Contract`, `ExecutionReport`, `AuditReport`, enums |
| Create `framework/mea/store.go` | `FileStore` under `{root}/{session}.json`, sanitize id, atomic write |
| Create `framework/mea/store_test.go` | Round-trip, path traversal reject, atomicity smoke |
| Create `framework/mea/apply.go` | `ApplyAudit(state, audit) → state`；only clean+complete may set `completed` |
| Create `framework/mea/apply_test.go` | Executor-equivalent updates rejected; audit gates |
| Create `framework/mea/rules_auditor.go` | Structured checks: `path_exists`, `file_contains`, `json_path`（M0）；optional `command_exit` whitelist later |
| Create `framework/mea/rules_auditor_test.go` | Temp dir fixtures |
| Create `framework/mea/orchestrator.go` | Loop: manager → exec → audit → apply → persist；`max_rounds` |
| Create `framework/mea/orchestrator_test.go` | Fake mgr/exec/aud；假完成被拦；多轮推进 |
| Create `framework/mea/manager_bootstrap.go` | M0 非 LLM：goal → one pending requirement；contract from `AcceptanceChecks` |
| Create `portal/internal/chat/mea_flag.go` | `MEAEnabled`, pilot list, data root setter |
| Create `portal/internal/chat/mea_flag_test.go` | Env matrix |
| Modify `portal/cmd/backend/main.go` | `chat.SetMEADataRoot(bc.Data.GetDataRoot())` near vector data root |
| Create `portal/docs/mea-m0.md` | Flag、路径、与 Procedural/Growth 边界 |
| Modify spec §15 / status | Link this plan；状态改为「M0 计划已确认」 |

**Out of scope (do not implement in this plan):**

- M1 LLM Auditor / write-tool ban enforcement beyond “rules auditor has no mutators”
- M2 SSE/UI TaskState
- Full production chat SSE 替换主 ReAct（可留 `RunIfEnabled` 钩子 + 单测；完整接线可 follow-up）
- MySQL / `memory_units` TaskState
- AgentAdapter for Claude Code / Codex
- Changing Growth or Procedural commit paths

---

### Task 1: Types (`framework/mea`)

**Files:**
- Create: `framework/mea/types.go`
- Test: `framework/mea/types_json_test.go`

- [ ] **Step 1: Write failing JSON round-trip test**

```go
package mea

import (
	"encoding/json"
	"testing"
)

func TestTaskStateJSON_RoundTrip(t *testing.T) {
	s := TaskState{
		Version:   1,
		SessionID: "sess-1",
		AgentID:   "agent-a",
		Goal:      "create out.txt with hello",
		Records: []TaskRecord{{
			ID:      "r1",
			Kind:    KindRequirement,
			Status:  StatusPending,
			Summary: "out.txt contains hello",
		}},
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var got TaskState
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Goal != s.Goal || len(got.Records) != 1 || got.Records[0].Status != StatusPending {
		t.Fatalf("got %+v", got)
	}
}
```

- [ ] **Step 2: Run test — expect fail (package missing)**

```bash
cd framework
go test ./mea/ -run TestTaskStateJSON_RoundTrip -count=1
```

Expected: fail to compile / no package

- [ ] **Step 3: Implement `types.go`**

```go
package mea

import "time"

const (
	KindRequirement = "requirement"
	KindArtifact    = "artifact"
	KindFact        = "fact"

	StatusPending    = "pending"
	StatusCompleted  = "completed"
	StatusBlocked    = "blocked"
	StatusUntrusted  = "untrusted"

	CompletionComplete   = "complete"
	CompletionIncomplete = "incomplete"
	CompletionBlocked    = "blocked"

	IntegrityClean     = "clean"
	IntegritySuspect   = "suspect"
	IntegrityViolation = "violation"

	DecisionExecute = "execute"
	DecisionDone    = "done"
	DecisionBlocked = "blocked"
	DecisionAsk     = "ask"
)

type TaskRecord struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	Summary      string   `json:"summary"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

type AcceptanceCheck struct {
	Type    string `json:"type"` // path_exists | file_contains | json_path
	Path    string `json:"path,omitempty"`
	Pattern string `json:"pattern,omitempty"` // file_contains
	JSONPath string `json:"json_path,omitempty"`
	Equals  string `json:"equals,omitempty"`
}

type Contract struct {
	Round             int               `json:"round"`
	Goal              string            `json:"goal"`
	Acceptance        []string          `json:"acceptance"`
	AcceptanceChecks  []AcceptanceCheck `json:"acceptance_checks,omitempty"`
	Boundaries        []string          `json:"boundaries,omitempty"`
	RelevantStateIDs  []string          `json:"relevant_state_ids,omitempty"`
	PriorAuditIDs     []string          `json:"prior_audit_ids,omitempty"`
	ToolHint          string            `json:"tool_hint,omitempty"`
	TargetRecordID    string            `json:"target_record_id,omitempty"`
}

type ExecutionReport struct {
	Round            int      `json:"round"`
	Summary          string   `json:"summary"`
	ArtifactsTouched []string `json:"artifacts_touched,omitempty"`
	Issues           []string `json:"issues,omitempty"`
	ClaimComplete    bool     `json:"claim_complete,omitempty"` // ignored for state writes
}

type ProposedUpdate struct {
	RecordID string `json:"record_id,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Status   string `json:"status"`
	Summary  string `json:"summary,omitempty"`
}

type Evidence struct {
	Type    string `json:"type"`
	Ref     string `json:"ref,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
}

type AuditReport struct {
	ID              string           `json:"id"`
	Round           int              `json:"round"`
	Completion      string           `json:"completion"`
	Integrity       string           `json:"integrity"`
	ProposedUpdates []ProposedUpdate `json:"proposed_updates,omitempty"`
	Evidence        []Evidence       `json:"evidence,omitempty"`
}

type TaskState struct {
	Version   int           `json:"version"`
	SessionID string        `json:"session_id"`
	AgentID   string        `json:"agent_id"`
	Goal      string        `json:"goal"`
	Records   []TaskRecord  `json:"records"`
	Audits    []AuditReport `json:"audits,omitempty"`
	UpdatedAt time.Time     `json:"updated_at"`
}
```

- [ ] **Step 4: Re-run test — PASS**

```bash
cd framework
go test ./mea/ -run TestTaskStateJSON_RoundTrip -count=1
```

- [ ] **Step 5: Commit only if user asked**

---

### Task 2: FileStore

**Files:**
- Create: `framework/mea/store.go`
- Test: `framework/mea/store_test.go`

- [ ] **Step 1: Failing tests**

```go
package mea

import (
	"path/filepath"
	"testing"
	"time"
)

func TestFileStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	st := NewFileStore(dir)
	s := TaskState{Version: 1, SessionID: "abc", Goal: "g", UpdatedAt: time.Now().UTC()}
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	got, err := st.Load("abc")
	if err != nil || got.Goal != "g" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "abc.json")); err != nil {
		t.Fatal(err)
	}
}

func TestFileStore_RejectUnsafeSessionID(t *testing.T) {
	st := NewFileStore(t.TempDir())
	err := st.Save(TaskState{SessionID: "../x", Version: 1})
	if err == nil {
		t.Fatal("expected error")
	}
}
```

（补 `os` import。）

- [ ] **Step 2: Run — FAIL**

```bash
cd framework
go test ./mea/ -run TestFileStore_ -count=1
```

- [ ] **Step 3: Implement `store.go`**

要点：

- `NewFileStore(root string)`；root 为空报错或 Save 报错  
- `sanitizeSessionID`：仅允许 `[A-Za-z0-9._-]`，长度上限（如 128）  
- `Save`：写 `root/<id>.json.tmp` 再 `os.Rename`  
- `Load`：不存在返回明确 `ErrNotFound`（`errors.Is`）

- [ ] **Step 4: PASS**

- [ ] **Step 5: Commit only if user asked**

---

### Task 3: ApplyAudit 门闩

**Files:**
- Create: `framework/mea/apply.go`
- Test: `framework/mea/apply_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestApplyAudit_CompleteRequiresClean(t *testing.T) {
	s := TaskState{Records: []TaskRecord{{ID: "r1", Kind: KindRequirement, Status: StatusPending}}}
	out := ApplyAudit(s, AuditReport{
		ID: "a1", Completion: CompletionComplete, Integrity: IntegritySuspect,
		ProposedUpdates: []ProposedUpdate{{RecordID: "r1", Status: StatusCompleted}},
	})
	if out.Records[0].Status == StatusCompleted {
		t.Fatal("suspect must not complete")
	}
}

func TestApplyAudit_CleanCompleteUpdates(t *testing.T) {
	s := TaskState{Records: []TaskRecord{{ID: "r1", Status: StatusPending}}}
	out := ApplyAudit(s, AuditReport{
		ID: "a1", Completion: CompletionComplete, Integrity: IntegrityClean,
		ProposedUpdates: []ProposedUpdate{{RecordID: "r1", Status: StatusCompleted, Summary: "ok"}},
	})
	if out.Records[0].Status != StatusCompleted {
		t.Fatal(out.Records[0].Status)
	}
	if len(out.Audits) != 1 || out.Records[0].EvidenceRefs[0] != "a1" {
		t.Fatal("audit not linked")
	}
}

func TestNoApplyExecutionReportAPI(t *testing.T) {
	// Compile-time / package convention: ExecutionReport must not write TaskState.
	// Ensure ApplyAudit is the only mutator used in orchestrator (grep in review).
	s := TaskState{Records: []TaskRecord{{ID: "r1", Status: StatusPending}}}
	_ = ExecutionReport{ClaimComplete: true}
	if s.Records[0].Status != StatusPending {
		t.Fatal("execution claim must not mutate state")
	}
}
```

- [ ] **Step 2–4:** 实现 `ApplyAudit`：append audit；仅当 `completion==complete && integrity==clean` 时应用 `StatusCompleted`；`incomplete/blocked` 可写 `pending/blocked/untrusted`；更新 `UpdatedAt`。**禁止**导出 `ApplyExecutionReport`。

- [ ] **Step 5: Commit only if user asked**

---

### Task 4: RulesAuditor

**Files:**
- Create: `framework/mea/rules_auditor.go`
- Test: `framework/mea/rules_auditor_test.go`

- [ ] **Step 1: Failing tests with temp workspace**

```go
func TestRulesAuditor_PathExistsAndContains(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "out.txt")
	os.WriteFile(p, []byte("hello"), 0o644)
	aud := RulesAuditor{WorkDir: root}
	c := Contract{
		Round: 1,
		TargetRecordID: "r1",
		AcceptanceChecks: []AcceptanceCheck{
			{Type: "path_exists", Path: "out.txt"},
			{Type: "file_contains", Path: "out.txt", Pattern: "hello"},
		},
	}
	v, err := aud.Audit(context.Background(), TaskState{}, c, ExecutionReport{ClaimComplete: true})
	if err != nil {
		t.Fatal(err)
	}
	if v.Completion != CompletionComplete || v.Integrity != IntegrityClean {
		t.Fatalf("%+v", v)
	}
}

func TestRulesAuditor_RejectsFalseClaim(t *testing.T) {
	aud := RulesAuditor{WorkDir: t.TempDir()}
	c := Contract{TargetRecordID: "r1", AcceptanceChecks: []AcceptanceCheck{{Type: "path_exists", Path: "missing.txt"}}}
	v, _ := aud.Audit(context.Background(), TaskState{}, c, ExecutionReport{ClaimComplete: true})
	if v.Completion == CompletionComplete {
		t.Fatal("must not complete")
	}
}

func TestRulesAuditor_JSONPath(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "meta.json"), []byte(`{"ok":true,"n":1}`), 0o644)
	aud := RulesAuditor{WorkDir: root}
	c := Contract{
		TargetRecordID: "r1",
		AcceptanceChecks: []AcceptanceCheck{
			{Type: "json_path", Path: "meta.json", JSONPath: "ok", Equals: "true"},
		},
	}
	v, err := aud.Audit(context.Background(), TaskState{}, c, ExecutionReport{})
	if err != nil || v.Completion != CompletionComplete {
		t.Fatalf("err=%v v=%+v", err, v)
	}
}

func TestRulesAuditor_RejectPathEscape(t *testing.T) {
	aud := RulesAuditor{WorkDir: t.TempDir()}
	c := Contract{AcceptanceChecks: []AcceptanceCheck{{Type: "path_exists", Path: "../secret"}}}
	v, _ := aud.Audit(context.Background(), TaskState{}, c, ExecutionReport{})
	if v.Completion == CompletionComplete {
		t.Fatal("path escape must fail audit")
	}
}
```

- [ ] **Step 2–4:** 实现  
  - 路径必须 `filepath.Join(WorkDir, path)` 后仍在 WorkDir 内（`Reject` `..`）  
  - 支持 `path_exists` / `file_contains` / `json_path`（M0 顶层 key 即可：读 JSON object，按 `JSONPath` 单段 key 取值与 `Equals` 字符串比较；不必上完整 JSONPath 引擎）  
  - 全部 check 通过 → complete+clean + `ProposedUpdates` completed for `TargetRecordID`  
  - 否则 incomplete（不得 completed）；路径逃逸 → incomplete + `integrity=violation`  
  - **不实现** 任意 shell；`command_exit` 不做

- [ ] **Step 5: Commit only if user asked**

---

### Task 5: Bootstrap Manager + Orchestrator

**Files:**
- Create: `framework/mea/manager_bootstrap.go`
- Create: `framework/mea/orchestrator.go`
- Test: `framework/mea/orchestrator_test.go`

- [ ] **Step 1: Define interfaces in `orchestrator.go`**

```go
type Manager interface {
	Decide(ctx context.Context, s TaskState) (decision string, contract *Contract, state TaskState, err error)
}

type Executor interface {
	Execute(ctx context.Context, s TaskState, c Contract) (ExecutionReport, error)
}

type Auditor interface {
	Audit(ctx context.Context, s TaskState, c Contract, o ExecutionReport) (AuditReport, error)
}
```

- [ ] **Step 2: `BootstrapManager`（规格 §4.2 / §6）**

- 构造参数：`Goal`、`Checks []AcceptanceCheck`（可观察验收；**不得为空**）  
- **若 `len(Checks)==0`：一律 `decision=ask`（或返回 error `ErrNoObservableAcceptance`），不得 `execute`**  
- 若 `Records` 空：创建一条 `requirement`，附带传入的 Checks  
- 若有 pending：对第一条 pending 发 `execute` + contract（拷贝该 record 的 checks / TargetRecordID）  
- 若无 pending 且无 blocked：`done`  
- 存在 blocked 且无可执行 pending：`blocked`  
- `Orchestrator.MaxRounds` 默认 **25**（规格 §6）；`<=0` 时采用默认

- [ ] **Step 3: Orchestrator 金样例测试（覆盖规格 §14.1）**

至少下列用例（可拆多个 `TestOrchestrator_*`）：

1. **假完成×3（可参数化 table）**：executor `ClaimComplete=true` 但缺文件 / 内容不匹配 / json 不对 → 状态仍 `pending`，最终非 `done`（或 rounds 耗尽仍未 completed）  
2. **多步推进×2**：  
   - A：先谎报再真实写文件 → 第二轮 `completed` → `done`  
   - B：两条 requirement（两个 checks / 两轮 execute）→ 依次完成后 `done`

```go
func TestBootstrapManager_NoChecksAsk(t *testing.T) {
	mgr := BootstrapManager{Goal: "x", Checks: nil}
	d, c, _, err := mgr.Decide(context.Background(), TaskState{SessionID: "s", Goal: "x"})
	if err != nil && !errors.Is(err, ErrNoObservableAcceptance) {
		// either ask decision or typed error is OK
	}
	if d == DecisionExecute || c != nil {
		t.Fatal("must not execute without observable acceptance")
	}
	if d != DecisionAsk && err == nil {
		t.Fatal("expected ask or error")
	}
}

func TestOrchestrator_FalseClaimThenSuccess(t *testing.T) {
	dir := t.TempDir()
	meaDir := filepath.Join(dir, "mea")
	work := filepath.Join(dir, "work")
	os.MkdirAll(meaDir, 0o755)
	os.MkdirAll(work, 0o755)
	store := NewFileStore(meaDir)
	checks := []AcceptanceCheck{{Type: "path_exists", Path: "out.txt"}}
	mgr := BootstrapManager{Goal: "create out.txt", Checks: checks}
	var round int
	exec := ExecutorFunc(func(ctx context.Context, s TaskState, c Contract) (ExecutionReport, error) {
		round++
		if round == 1 {
			return ExecutionReport{ClaimComplete: true, Summary: "lied"}, nil
		}
		os.WriteFile(filepath.Join(work, "out.txt"), []byte("x"), 0o644)
		return ExecutionReport{ClaimComplete: true, Summary: "wrote"}, nil
	})
	orch := Orchestrator{
		Store: store, Manager: mgr, Executor: exec,
		Auditor: RulesAuditor{WorkDir: work}, MaxRounds: 25,
	}
	final, reason, err := orch.Run(context.Background(), RunInput{
		SessionID: "sess-1", AgentID: "a", Goal: "create out.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reason != DecisionDone {
		t.Fatalf("reason=%s", reason)
	}
	if final.Records[0].Status != StatusCompleted {
		t.Fatal(final.Records[0].Status)
	}
}
```

另写 `TestOrchestrator_FalseClaimVariants`（table：missing path / wrong contains / wrong json）与 `TestOrchestrator_TwoRequirementsSequential`。

- [ ] **Step 4: `Orchestrator.Run`**：load or init state → loop Decide →（`ask`/`done`/`blocked` 立即返回）→ Execute → Audit → ApplyAudit → Save；尊重 `MaxRounds`（默认 25）；返回最终 state + 终止 reason。`MaxRounds` 耗尽时 reason 固定为 **`max_rounds`**（便于假完成 table 断言）。提供 `type ExecutorFunc func(...) (ExecutionReport, error)` 并实现 `Executor` 接口，供测试使用。

另：`TestOrchestrator_TwoRequirementsSequential` **预置** `TaskState.Records` 为两条 pending requirement（各带自己的 `TargetRecordID`），用 **fake Manager**（按 pending 顺序发 contract + 对应 Checks），不要依赖只建一条 record 的 `BootstrapManager`。

- [ ] **Step 5: PASS `go test ./mea/ -count=1`**

- [ ] **Step 6: Commit only if user asked**

---

### Task 6: Portal flag + data root

**Files:**
- Create: `portal/internal/chat/mea_flag.go`
- Create: `portal/internal/chat/mea_flag_test.go`
- Modify: `portal/cmd/backend/main.go`（在 `SetMemoryVectorDataRoot` 附近）
- Create: `portal/docs/mea-m0.md`

- [ ] **Step 1: Tests**

```go
func TestMEAEnabled_DefaultOff(t *testing.T) {
	t.Setenv("SATH_MEA", "")
	if MEAEnabled() {
		t.Fatal()
	}
}
func TestMEAEnabled_On(t *testing.T) {
	t.Setenv("SATH_MEA", "1")
	if !MEAEnabled() {
		t.Fatal()
	}
}
func TestMEAEnabledForAgent_Pilot(t *testing.T) {
	t.Setenv("SATH_MEA", "0")
	t.Setenv("SATH_MEA_PILOT_AGENTS", "agent-a,agent-b")
	if !MEAEnabledForAgent("agent-a") {
		t.Fatal()
	}
	if MEAEnabledForAgent("other") {
		t.Fatal()
	}
}
```

- [ ] **Step 2: Implement**

- `SATH_MEA=1|true` → 全局开  
- `SATH_MEA_PILOT_AGENTS` 逗号列表 → 仅列表内 agent（即使全局 off 也可 pilot-only：定义清晰——**推荐**：`MEAEnabledForAgent` = 全局开 **或** pilot 命中）  
- `SetMEADataRoot` / `MEAFileStore()` → `filepath.Join(dataRoot, "mea")`  
- `MEAWorkDir` 可选后续；M0 文档写明 workdir 由调用方传入 RulesAuditor

- [ ] **Step 3: `main.go` 调用 `chat.SetMEADataRoot(...)`**

- [ ] **Step 4: `portal/docs/mea-m0.md`** — flag、路径、边界、如何跑 `go test`

- [ ] **Step 5: `cd portal; go test ./internal/chat/ -run TestMEA -count=1`**

- [ ] **Step 6: Commit only if user asked**

---

### Task 7: Portal 可调用入口（薄封装，不改默认 SSE 行为）

**Files:**
- Create: `portal/internal/chat/mea_run.go`
- Create: `portal/internal/chat/mea_run_test.go`

- [ ] **Step 1:** `RunRulesMEA(ctx, input)`：若 `!MEAEnabledForAgent` → 返回 `skipped`；否则用 `MEAFileStore` + `BootstrapManager` + 调用方传入的 `Executor` + `RulesAuditor{WorkDir}` 跑 Orchestrator  

- [ ] **Step 2:** 单测用 fake Executor + temp data root，**不**启动 HTTP  

- [ ] **Step 3:** **不**在 `service/chat.go` 默认流式路径自动接管（避免行为变化）；在 `mea-m0.md` 写明 follow-up：流式路径挂钩  

- [ ] **Step 4: Commit only if user asked**

---

### Task 8: Spec 回链与自检

**Files:**
- Modify: `docs/superpowers/specs/2026-08-12-mea-minimal-subset-design.md`  
  - 状态 → `M0 计划已确认`  
  - §15 链到本 plan  

- [ ] **Step 1: 编辑回链**

- [ ] **Step 2: 全量**

```bash
cd framework
go test ./mea/ -count=1
cd ../portal
go test ./internal/chat/ -run TestMEA -count=1
```

Expected: PASS

- [ ] **Step 3: Commit only if user asked**（可含 spec + plan + code）

---

## 验收对照（规格 §14.1）

| 规格项 | 本计划覆盖 |
|--------|------------|
| TaskState 读写 / 非法 session_id | Task 2 |
| 仅 audit/机检可 completed | Task 3–5 |
| flag 关无行为变化 | Task 6–7（默认不接管 chat） |
| 假完成被拦（≥3） | Task 5 table：missing / contains / json |
| 多步推进（≥2） | Task 5：谎报后成功；双 requirement 顺序完成 |
| 无可观察 acceptance 不 execute | Task 5 `TestBootstrapManager_NoChecksAsk` |
| 观测事件 §11 | M0 **可选** `slog`；不阻塞本计划 |

---

## 执行备注

- `framework/` 与 `portal/` 若为嵌套 git 工作区，在各自目录跑测试；本 monorepo 文档在仓库根 `docs/`。  
- 实现时遵守用户规则：**未经要求不要 commit**。  
- M1/M2 另开 plan，勿塞进本文件。
