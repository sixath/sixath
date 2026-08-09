# Growth System (A+B) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `@superpowers/subagent-driven-development` (recommended) or `@superpowers/executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver spec `docs/superpowers/specs/2026-05-10-growth-system-design.md`: per-session growth counters, workspace lease, atomic skill patches, memory refresh via existing paths, events for observability, and recursive-safe review execution—without FTS in v1.

**Architecture:** `framework/growth` holds pure logic (patch applier, JSON schema, optional review runner), `framework/agent` gains an optional post-tool-success hook threaded with `*Request`, `portal` persists `ChatGrowthState` + `GrowthWorkspaceLease`, runs async dequeue after chat/stream completion, and uses MySQL row compare for lease acquisition.

**Tech Stack:** Go 1.22+, GORM AutoMigrate (`portal/internal/data/data.go`), existing `github.com/sixath/framework/{agent,events,tool,memory}`, Kratos portal layout.

**Spec reference:** `docs/superpowers/specs/2026-05-10-growth-system-design.md`（本仓库 `framework` 根下相对路径）

**二期（功能清单、评审、里程碑）**：[`2026-05-10-growth-system-phase2.md`](./2026-05-10-growth-system-phase2.md)

---

## File map (create / modify)

| Area | Path | Responsibility |
|------|------|----------------|
| Events | `framework/events/event.go` | Add `GrowthReviewScheduled`, `GrowthReviewCompleted`, `GrowthReviewFailed` |
| Growth core | `framework/growth/patch.go` | JSON types: `PatchOp`, `SkillPatchBatch` |
| Growth core | `framework/growth/applier.go` | Apply batch under workspace root with tmp+rename |
| Growth core | `framework/growth/config.go` | Default thresholds, lease TTL |
| Growth core | `framework/growth/applier_test.go` | Table-driven fs tests |
| Agent hook | `framework/agent/react_agent.go` | Thread `req` into `executeToolStep`; call `ToolSuccessHook` after successful tool |
| Agent hook | `framework/agent/react_agent.go` (ReActConfig / options) | `WithReActToolSuccessHook` |
| Agent tests | `framework/agent/react_agent_test.go` | Hook invoked once per successful tool |
| Portal model | `portal/internal/data/model/growth.go` | `ChatGrowthState`, `GrowthWorkspaceLease` structs + `TableName` |
| Portal data | `portal/internal/data/data.go` | AutoMigrate new models |
| Portal repo | `portal/internal/data/growth_mysql.go` (new) | CRUD + lease acquire/release with `WHERE expires_at` / instance id |
| Portal biz | `portal/internal/biz/growth.go` (new) | Usecase: bump counters, try schedule review |
| Portal service | `portal/internal/service/chat.go` | Pass hook into `BuildReActAgent` options; on stream done call growth trySchedule |
| Portal chat | `portal/internal/chat/agent_builder.go` | Extend `BuildReActAgent` to accept optional `...agent.ReActOption` from caller |
| Wire | `portal/cmd/backend/wire.go` + `wire_gen.go` | Inject growth repo / usecase as needed |

---

### Task 1: Growth event kinds

**Files:**

- Modify: `framework/events/event.go`
- Test: `framework/events/bus_test.go` (extend or add `event_kind_test.go` if preferred)

- [ ] **Step 1: Add three `Kind` constants** after existing agent kinds:

```go
	GrowthReviewScheduled Kind = "growth.review.scheduled"
	GrowthReviewCompleted Kind = "growth.review.completed"
	GrowthReviewFailed    Kind = "growth.review.failed"
```

- [ ] **Step 2: Run framework tests**

Run:

```bash
cd framework
go test ./events/... -count=1
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
cd framework
git add events/event.go events/bus_test.go
git commit -m "feat(events): add growth review event kinds"
```

---

### Task 2: Patch schema + validation (no filesystem yet)

**Files:**

- Create: `framework/growth/patch.go`
- Create: `framework/growth/patch_test.go`

- [ ] **Step 1: Write failing test** `TestValidatePatchBatch_rejectsPathOutsideWorkspace`

```go
func TestValidatePatchBatch_rejectsPathOutsideWorkspace(t *testing.T) {
	err := validatePatchBatch("/work", []Patch{{Path: "../etc/passwd", Op: OpCreate, Content: "x"}})
	if err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: Run test — expect FAIL** (`undefined: Patch` etc.)

Run: `cd framework && go test ./growth/... -count=1`  
Expected: FAIL compile or FAIL assertion

- [ ] **Step 3: Implement** `Patch`, `Op` enum (`create|patch|delete`), `validatePatchBatch(root, batch) error` resolving `filepath.Clean` + `strings.HasPrefix` after `filepath.EvalSymlinks` optional skip on Windows tests using `t.TempDir()` only.

- [ ] **Step 4: Run test — PASS**

Run: `cd framework && go test ./growth/... -run TestValidatePatchBatch -v`

- [ ] **Step 5: Commit** `feat(growth): add patch batch validation`

---

### Task 3: Atomic applier

**Files:**

- Create: `framework/growth/applier.go`
- Modify: `framework/growth/patch_test.go` or `applier_test.go`

- [ ] **Step 1: Test** `TestApplyPatchBatch_atomicRollbackOnMidFailure` — first file ok, second invalid → first file unchanged.

- [ ] **Step 2: Implement** `ApplyPatchBatch(workspaceRoot string, batch []Patch) error` using `os.CreateTemp` in target dir, `io.WriteString`, `os.Rename`, for `patch` op read-modify-write with unique old_string requirement documented in code comment.

- [ ] **Step 3:** `go test ./growth/... -count=1`

- [ ] **Step 4: Commit** `feat(growth): add atomic skill patch applier`

---

### Task 4: Default config

**Files:**

- Create: `framework/growth/config.go`
- Test: `framework/growth/config_test.go` (optional, defaults only)

- [ ] **Step 1:** Define `Defaults struct { SkillToolInterval int; MemoryTurnInterval int; LeaseTTL time.Duration }` with documented defaults matching spec (e.g. tool success every 10 iterations triggers pending skill review — tune in PR).

- [ ] **Step 2:** `go test ./growth/...`

- [ ] **Step 3: Commit** `feat(growth): add default thresholds and lease TTL`

---

### Task 5: ReAct `ToolSuccessHook` + `req` threading

**Files:**

- Modify: `framework/agent/react_agent.go` — `ReActConfig` add `ToolSuccessHook func(context.Context, *Request, ToolCallRecord)`; `executeToolStep(ctx, req, ...)`, `executeOneToolCall` call hook when `record.Error == "" && record.Allowed` after `ToolCompleted` emit.
- Modify: all `executeToolStep` callers: `Run`, `runToolEventsSync`, `runToolEvents` — pass `req` (RunEvents goroutine closes over `req`).
- Create: `framework/agent/react_hook_test.go` or extend `react_agent_test.go`

- [ ] **Step 1: Failing test** — spy hook counts successes when tool returns result without error.

- [ ] **Step 2: Implement** + `WithReActToolSuccessHook` in same file as other `WithReAct*` options.

- [ ] **Step 3:** `cd framework && go test ./agent/... -count=1`  
Expected: PASS (full agent suite)

- [ ] **Step 4: Commit** `feat(agent): add optional tool success hook with request context`

---

### Task 6: Portal models + AutoMigrate

**Files:**

- Create: `portal/internal/data/model/growth.go`
- Modify: `portal/internal/data/data.go` — add `&model.ChatGrowthState{}`, `&model.GrowthWorkspaceLease{}` to `AutoMigrate` slice

Suggested columns (adjust names to match Go/GORM style in implementation):

- `chat_growth_state`: `session_id` (PK/unique), `tool_iters_since_review`, `turns_since_memory_review`, `pending_skill_review`, `pending_memory_review`, `last_skill_error`, `last_memory_error`, `updated_at`
- `growth_workspace_lease`: `workspace_key` (unique), `holder_id`, `expires_at`, `updated_at`

- [ ] **Step 1:** Define structs with gorm tags.

- [ ] **Step 2:** `cd portal && go build ./...`

- [ ] **Step 3: Commit** `feat(portal): add growth state and workspace lease tables`

---

### Task 7: Growth repository + lease CAS

**Files:**

- Create: `portal/internal/data/growth_mysql.go`
- Create: `portal/internal/biz/growth.go` — thin wrappers / interfaces used by service

Methods (signatures indicative):

- `UpsertGrowthState(ctx, sessionID, fn func(*ChatGrowthState) error) error`
- `TryAcquireLease(ctx, workspaceKey, holderID string, ttl time.Duration) (bool, error)`
- `ReleaseLease(ctx, workspaceKey, holderID string) error`

Use transaction: `UPDATE growth_workspace_leases SET holder_id=?, expires_at=? WHERE workspace_key=? AND (holder_id='' OR expires_at<NOW())` pattern with rows affected check; exact SQL compatible with MySQL 8.

- [ ] **Step 1: Unit test** with sqlite — **if portal tests only MySQL**, document manual test + add integration tag `//go:build integration` or use docker in CI later; minimal path: table-driven test of SQL generation skipped if no DSN — **prefer** adding `testing` fake in-memory sqlite **only if** portal already uses sqlite in tests; else `go test` only compiles (`go test ./internal/data/...` with build tag).

- [ ] **Step 2: Implement** repo against `*gorm.DB`.

- [ ] **Step 3:** `cd portal && go build ./...`

- [ ] **Step 4: Commit** `feat(portal): add growth persistence and workspace lease`

---

### Task 8: Extend `BuildReActAgent` for extra options

**Files:**

- Modify: `portal/internal/chat/agent_builder.go` — change signature to:

```go
func BuildReActAgent(m model.Model, reg *tool.Registry, systemPrompt string, maxHistory int, extra ...agent.ReActOption) agent.Agent
```

Append `extra` to `opts` before `NewReActAgent`.

- Modify: all call sites (`portal/internal/service/chat.go`, `agent.go`, tests if any) — pass `nil` or variadic empty for now.

- [ ] **Step 1:** `cd portal && go build ./...`

- [ ] **Step 2: Commit** `refactor(portal): allow extra ReActOption in BuildReActAgent`

---

### Task 9: Wire hook from `ChatService` (counters only, no LLM review yet)

**Files:**

- Modify: `portal/internal/service/chat.go`
- Possibly: new `portal/internal/chat/growth_hooks.go` to keep `chat.go` smaller

Behavior:

- When building agent for a turn, pass `agent.WithReActToolSuccessHook(func(ctx context.Context, req *agent.Request, rec agent.ToolCallRecord) { ... })` that:
  - Reads `session_id` from `req.Metadata["session_id"]` (already set by `prefetchRequestMetadata` in chat.go — verify key name matches).
  - If `rec.Error != ""` **skip** (should not happen on hook path).
  - Calls biz growth `OnToolSuccess(ctx, sessionID)` to increment `tool_iters_since_review` and set `pending_skill_review` when threshold met (non-blocking DB write, log errors).

- On assistant message persisted / stream `onDone`, call `OnAssistantTurn(ctx, sessionID)` for `turns_since_memory_review`.

- [ ] **Step 1: Manual verification** — run portal locally, single chat with tool call, inspect DB row (document SQL in PR description).

- [ ] **Step 2: Commit** `feat(portal): increment growth counters on tool success and assistant turn`

---

### Task 10: Async review dequeue + lease + events (skeleton runner)

**Files:**

- Create: `portal/internal/service/growth_worker.go` or `internal/growth/worker.go`
- Modify: `portal/cmd/backend/main.go` (or wherever HTTP server starts) — start `go growthWorker.Loop(ctx)` with process-scoped `holderID = uuid` env.

Worker loop (every 30–60s or on signal channel from `OnToolSuccess` when pending):

1. Select sessions with `pending_skill_review OR pending_memory_review`.
2. For each, load agent workspace; `TryAcquireLease`.
3. On success, publish `GrowthReviewScheduled` via `events.DefaultBus()` if non-nil.
4. Invoke `growth.ReviewRunner` interface — **v1 stub** implementation `noopRunner` that immediately completes memory path by calling existing `chat.NotifyMemorySessionDirty` and clears pending flags without LLM.
5. Publish `GrowthReviewCompleted` or `GrowthReviewFailed`; `ReleaseLease`.

- [ ] **Step 1:** Implement stub runner in `framework/growth/runner_stub.go` as `type StubRunner struct{}` implementing `Runner` interface.

- [ ] **Step 2:** Wire in portal.

- [ ] **Step 3:** `go build` both modules.

- [ ] **Step 4: Commit** `feat(portal): add growth worker with lease and stub review runner`

---

### Task 11: Combined LLM review (optional follow-up PR)

**Files:**

- Create: `framework/growth/runner_llm.go`
- Depends on: portal `BuildModel` parity — inject `model.Model` + prompts

- [ ] **Step 1:** Prompt templates produce JSON matching `SkillPatchBatch` + optional memory hints (second phase: call portal callback interface).

- [ ] **Step 2:** Feature flag in portal config `growth.llm_enabled` default false until prompt QA done.

- [ ] **Step 3: Commit** `feat(growth): add optional LLM-backed review runner`

---

### Task 12: Idle sweep (optional, if Task 10 uses poll-only)

**Files:**

- Modify: worker loop already polls DB — ensure `last_idle_check_at` column OR reuse `updated_at` + `pending` flags so idle session flush does not require new table.

- Document in `portal/docs/` one paragraph: idle interval default 10m.

- [ ] **Commit** `docs(portal): document growth idle polling behavior`

---

## Plan self-review

- Tasks ordered so each commit leaves repo buildable.
- Task 9 depends on Task 8; Task 10 depends on Task 7 and 9.
- Hermes-style combined LLM deferred to Task 11 behind flag (spec §5.3).
- FTS explicitly excluded (spec §1).
- Reviewer subagent: not dispatched in this environment; human or future `@plan-document-reviewer` pass optional.

---

## Execution handoff

Plan complete and saved to `framework/docs/superpowers/plans/2026-05-10-growth-system.md`.

**Two execution options:**

1. **Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks. **REQUIRED SUB-SKILL:** `@superpowers/subagent-driven-development`

2. **Inline Execution** — run tasks sequentially in this session with checkpoints. **REQUIRED SUB-SKILL:** `@superpowers/executing-plans`

Which approach do you want?
