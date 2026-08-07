# Trajectory Utilization Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.  
> `framework/` and `portal/` are nested git repos; run tests and commits inside the touched repo.  
> **Do not commit unless the user asks.**

**Goal:** Persist sanitized TurnTraces, enable message-level anchored transcript recall (via `memory_recall` + Portal API), run C3 in-process background review with deferred Wake, feed async Growth TraceDigest, and persist compact-boundary messages — without ShareGPT/RL.

**Architecture:** `BuildTurnTrace` lives in framework; MySQL `turn_traces` + FTS tool projections in portal/sessionsearch. C3 fork runs only in Portal (`GrowthWorker`), delaying `growthwake.Wake` while `background_review.enabled`. Anchored search is a new message-level API alongside existing session-collapsed `Search`.

**Tech Stack:** Go; `framework/agent` RunTrace; `framework/sessionsearch` SQLite FTS5; Portal GORM MySQL; GrowthWorker / `growthwake`.

**Spec:** [docs/superpowers/specs/2026-08-05-trajectory-utilization-design.md](../specs/2026-08-05-trajectory-utilization-design.md) (§1–§7, §16; Phase 1 / P1-A…P1-F only).

**Out of scope (separate plan later):** Insights, Rewind UX, fork_session lineage, ShareGPT exporter body.

---

## File map

| File | Responsibility |
|------|----------------|
| Create `framework/agent/turn_trace.go` | `TurnTrace`, `TurnToolCall`, `BuildTurnTrace`, redact/truncate helpers |
| Create `framework/agent/turn_trace_test.go` | Builder + redaction tests |
| Create `framework/turntrace/store.go` | `Store` interface + `NoopExporter` / `TrajectoryExporter` |
| Create `portal/internal/data/model/turn_trace.go` | GORM `TurnTrace` row |
| Create `portal/internal/data/turn_trace_mysql.go` | `TurnTraceStore` impl (upsert, list, get) |
| Create `portal/internal/data/turn_trace_mysql_test.go` | SQLite/MySQL unit tests for store |
| Modify `portal/internal/data/data.go` | AutoMigrate turn_traces |
| Modify `framework/sessionsearch/types.go` | `MessageDoc.ToolName`; `AnchorOpts`, `AnchoredHit`; manager methods |
| Modify `framework/sessionsearch/index.go` | schema v2 + tool_name; `SearchAnchored`, `GetMessagesAround` |
| Create/Modify `framework/sessionsearch/*_test.go` | Anchored + schema migration tests |
| Modify `framework/sessionsearch/manager.go` | Delegate new APIs |
| Modify `portal/internal/chat/session_search.go` | Index tool projections helper |
| Modify `framework/memory/session_transcript.go` | Empty query → ListRecent; non-empty → SearchAnchored-shaped hits |
| Modify `framework/memory/store.go` / recall query | Optional anchor fields if needed |
| Modify `framework/tool/memory/store_tools.go` | `memory_recall` params; transcript JSON shape |
| Create `portal/internal/service/transcript_search_api.go` (or wire existing HTTP) | GET transcript search |
| Modify `framework/agent/react_agent.go` / `trace.go` | Expose post-run `Messages` snapshot on Response / Stream Done for C3 |
| Modify `portal/internal/data/model/growth.go` | in_flight / last_review_* columns |
| Modify `portal/internal/biz/growth.go` | Deferred Wake when C3 on; in_flight TTL clear |
| Modify `portal/internal/service/growth_agent_review.go` | Snapshot-based fork entry |
| Modify `portal/internal/service/growth_worker.go` | Worker gates + TraceDigest |
| Modify `portal/internal/service/chat.go` | Persist trace, compact boundary, spawn BG review after Done |
| Modify `portal/internal/service/growth_chat.go` | Hook timing vs deferred wake |
| Modify conf / yaml | `growth.background_review.*`, `trace.persist.*` |

---

### Task 1: BuildTurnTrace + redaction (framework)

**Files:**
- Create: `framework/agent/turn_trace.go`
- Test: `framework/agent/turn_trace_test.go`

- [ ] **Step 1: Write failing tests**

```go
package agent

import (
	"strings"
	"testing"
)

func TestBuildTurnTrace_UnwrapsToolCallBridge(t *testing.T) {
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		Step: 0, ToolCallID: "c1", ToolName: "execute_read",
		Arguments: map[string]any{"sql": "select 1"},
		Result:    map[string]any{"rows": []any{}},
	}}}
	// Simulate already-unwrapped record (react sets ToolName=inner).
	out := BuildTurnTrace(TurnTraceMeta{SessionID: "s", AgentID: "a", RequestID: "r1"}, tr)
	if len(out.Calls) != 1 || out.Calls[0].ToolName != "execute_read" {
		t.Fatalf("%+v", out)
	}
	if out.RequestID != "r1" {
		t.Fatal(out.RequestID)
	}
}

func TestBuildTurnTrace_RedactsSecretKeysAndTruncates(t *testing.T) {
	big := strings.Repeat("x", 10_000)
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "http",
		Arguments: map[string]any{"password": "secret", "q": "ok"},
		Result:    big,
	}}}
	out := BuildTurnTrace(TurnTraceMeta{SessionID: "s", AgentID: "a", RequestID: "r"}, tr)
	if out.Calls[0].Arguments["password"] != "[redacted]" {
		t.Fatalf("args: %#v", out.Calls[0].Arguments)
	}
	if len(out.Calls[0].ResultPreview) > 4096+32 {
		t.Fatalf("preview too long: %d", len(out.Calls[0].ResultPreview))
	}
}

func TestBuildTurnTrace_NilTrace(t *testing.T) {
	if BuildTurnTrace(TurnTraceMeta{}, nil) != nil {
		t.Fatal("expected nil")
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
cd framework
go test ./agent/ -run TestBuildTurnTrace -count=1
```

Expected: FAIL (undefined `BuildTurnTrace`)

- [ ] **Step 3: Implement `turn_trace.go`**

```go
package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type TurnTraceMeta struct {
	SessionID, AgentID, RequestID string
}

type TurnTrace struct {
	SessionID string         `json:"session_id"`
	AgentID   string         `json:"agent_id"`
	RequestID string         `json:"request_id"`
	TurnSeq   int            `json:"turn_seq"` // assigned by store
	CreatedAt time.Time      `json:"created_at"`
	Calls     []TurnToolCall `json:"calls"`
}

type TurnToolCall struct {
	Step          int            `json:"step"`
	ToolCallID    string         `json:"tool_call_id"`
	ToolName      string         `json:"tool_name"`
	BridgeName    string         `json:"bridge_name,omitempty"`
	Arguments     map[string]any `json:"arguments,omitempty"`
	ResultPreview string         `json:"result_preview,omitempty"`
	Error         string         `json:"error,omitempty"`
	Blocked       bool           `json:"blocked,omitempty"`
	Decision      string         `json:"decision,omitempty"`
	DurationMS    int64          `json:"duration_ms,omitempty"`
}

const (
	maxArgBytes    = 2048
	maxResultRunes = 4096
	maxCalls       = 40
)

func BuildTurnTrace(meta TurnTraceMeta, tr *RunTrace) *TurnTrace {
	if tr == nil || len(tr.ToolCalls) == 0 {
		// Still allow empty calls for compact-only turns? Spec: persist when RunTrace present.
		if tr == nil {
			return nil
		}
	}
	out := &TurnTrace{
		SessionID: meta.SessionID,
		AgentID:   meta.AgentID,
		RequestID: meta.RequestID,
		CreatedAt: time.Now().UTC(),
	}
	recs := tr.ToolCalls
	// Prefer failures when truncating
	if len(recs) > maxCalls {
		recs = preferFailedThenTrim(recs, maxCalls)
	}
	for _, r := range recs {
		out.Calls = append(out.Calls, TurnToolCall{
			Step: r.Step, ToolCallID: r.ToolCallID, ToolName: r.ToolName,
			Arguments: redactArgs(r.Arguments), ResultPreview: previewResult(r.Result),
			Error: r.Error, Blocked: r.Blocked, Decision: r.Decision, DurationMS: r.DurationMS,
		})
	}
	return out
}

// redactArgs, previewResult, preferFailedThenTrim: implement per spec §4.3
```

Fill helpers: secret key substrings `password|token|secret|api_key|authorization` (case-insensitive); truncate JSON args to `maxArgBytes`; result via `fmt.Sprint` / JSON then rune truncate.

- [ ] **Step 4: Re-run tests — expect PASS**

```bash
cd framework
go test ./agent/ -run TestBuildTurnTrace -count=1
```

- [ ] **Step 5: Commit (only if user asked)**

```bash
cd framework
git add agent/turn_trace.go agent/turn_trace_test.go
git commit -m "feat(agent): BuildTurnTrace with redaction for trajectory persist"
```

---

### Task 2: TurnTraceStore interface + Noop exporter (framework)

**Files:**
- Create: `framework/turntrace/store.go`
- Create: `framework/turntrace/store_test.go`

- [ ] **Step 1: Write interface + noop test**

```go
package turntrace

import (
	"context"
	"testing"

	"github.com/sixath/framework/agent"
)

type Store interface {
	Upsert(ctx context.Context, t *agent.TurnTrace) error
	GetByRequest(ctx context.Context, sessionID, requestID string) (*agent.TurnTrace, error)
	ListBySession(ctx context.Context, sessionID string, limit int) ([]agent.TurnTrace, error)
}

type TrajectoryExporter interface {
	Export(ctx context.Context, input any) error
}

type NoopExporter struct{}

func (NoopExporter) Export(context.Context, any) error { return nil }

func TestNoopExporter(t *testing.T) {
	if err := (NoopExporter{}).Export(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}
```

Note: keep `agent.TurnTrace` in `agent` package to avoid portal importing a cycle; `turntrace` only holds interfaces.

- [ ] **Step 2: `go test ./turntrace/ -count=1` — PASS**

- [ ] **Step 3: Commit if asked**

---

### Task 3: MySQL turn_traces + Portal store (portal)

**Files:**
- Create: `portal/internal/data/model/turn_trace.go`
- Create: `portal/internal/data/turn_trace_mysql.go`
- Create: `portal/internal/data/turn_trace_mysql_test.go`
- Modify: `portal/internal/data/data.go` (AutoMigrate)

- [ ] **Step 1: Model**

```go
package model

import "time"

type TurnTraceRow struct {
	ID        string    `gorm:"column:id;primaryKey;size:36"`
	SessionID string    `gorm:"column:session_id;size:36;uniqueIndex:uk_session_request;index"`
	AgentID   string    `gorm:"column:agent_id;size:128;index"`
	RequestID string    `gorm:"column:request_id;size:64;uniqueIndex:uk_session_request"`
	TurnSeq   int       `gorm:"column:turn_seq;not null"`
	Payload   string    `gorm:"column:payload_json;type:longtext"`
	Active    bool      `gorm:"column:active;not null;default:1"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (TurnTraceRow) TableName() string { return "turn_traces" }
```

- [ ] **Step 2: Failing test — Upsert idempotent on (session_id, request_id)**

Use existing portal sqlite test helper pattern from `growth_mysql` / procedural tests: AutoMigrate, Upsert twice same request_id → one row, TurnSeq unchanged, payload updated.

- [ ] **Step 3: Implement repo**

- Allocate `TurnSeq` with transaction: `SELECT COALESCE(MAX(turn_seq),0)+1 ... FOR UPDATE` on session (or retry on conflict).
- Same request_id: UPDATE payload only, keep TurnSeq.
- `ListBySession`: `active=true`, order `turn_seq DESC`, limit.

- [ ] **Step 4: Wire AutoMigrate `&model.TurnTraceRow{}`**

- [ ] **Step 5: Run**

```bash
cd portal
go test ./internal/data/ -run TurnTrace -count=1
```

Expected: PASS

- [ ] **Step 6: Commit if asked**

---

### Task 4: Wire persist after chat Run (portal)

**Files:**
- Create: `portal/internal/chat/turn_trace_persist.go`
- Modify: `portal/internal/service/chat.go` (SendMessage + Stream Done paths)
- Test: `portal/internal/chat/turn_trace_persist_test.go`

- [ ] **Step 1: Helper**

```go
func PersistTurnTraceIfEnabled(ctx context.Context, store turntrace.Store, meta agent.TurnTraceMeta, tr *agent.RunTrace) {
    if store == nil || tr == nil { return }
    // read config trace.persist.enabled default true
    tt := agent.BuildTurnTrace(meta, tr)
    if tt == nil { return }
    if err := store.Upsert(ctx, tt); err != nil {
        log.Printf("turn_trace upsert failed: %v", err)
    }
}
```

- [ ] **Step 2: Call after successful Run** when `resp.Metadata["trace"]` or stream `Done` carries `*RunTrace`; pass `session_id`, `agent_id`, `request_id` from context.

- [ ] **Step 3: Unit test** with fake Store recording Upsert; ensure failure in Upsert does not return error to caller.

- [ ] **Step 4: Commit if asked**

---

### Task 5: FTS schema v2 + SearchAnchored (framework)

**Files:**
- Modify: `framework/sessionsearch/types.go`
- Modify: `framework/sessionsearch/index.go`
- Modify: `framework/sessionsearch/manager.go`
- Modify: `framework/sessionsearch/index_test.go`

- [ ] **Step 1: Extend types**

```go
type MessageDoc struct {
	ID, SessionID, Role, Content string
	ToolName  string
	CreatedAt time.Time
}

type AnchorOpts struct{ Window, Bookend int }

type AnchoredHit struct {
	SessionID, RootSessionID, Title string
	Anchor MessageDoc
	Window, BookendStart, BookendEnd []MessageDoc
	Score float64
}
```

Add to `SessionSearchManager`:
`SearchAnchored(ctx, SearchOpts, AnchorOpts) ([]AnchoredHit, error)`  
`GetMessagesAround(ctx, agentID, sessionID, messageID string, window int) ([]MessageDoc, error)`

- [ ] **Step 2: Schema bump**

Change `schemaMetaKey` to `session_index_schema_v2`. Migration strategy (pick one, document in code):

- **Preferred:** if meta version `<2`, `ALTER TABLE messages ADD COLUMN tool_name TEXT NOT NULL DEFAULT ''`; recreate FTS to include `tool_name` OR concatenate tool_name into indexed `content` only (simpler: keep FTS on content, store `tool_name` column for filters). Spec allows Content to include `tool=name ...`.

Minimal approach matching YAGNI: store `tool_name` column; FTS still indexes `content` which already embeds tool name; no FTS rebuild required beyond ADD COLUMN.

- [ ] **Step 3: Tests**

```go
func TestSearchAnchored_ToolProjection(t *testing.T) {
	// index user + tool message; query tool name; expect AnchoredHit with window
}
func TestSearchAnchored_NoCollapseMultipleSessions(t *testing.T) {
	// two sessions match; return up to limit hits message-level
}
```

- [ ] **Step 4: Implement SearchAnchored**

1. FTS query like `searchFTS` but return message ids (do **not** call `collapseHits`).
2. For each hit (limit 3–5): load ±Window messages for session by `created_at`; bookend first/last N user+assistant.
3. Dedupe: best score per session_id.

- [ ] **Step 5: `go test ./sessionsearch/ -count=1` — PASS**

---

### Task 6: Index tool projections after Upsert (portal)

**Files:**
- Modify: `portal/internal/chat/session_search.go` or `turn_trace_persist.go`
- Test: portal unit with fake SessionSearchManager

- [ ] **Step 1:** After successful Upsert, for each call:

```go
doc := sessionsearch.MessageDoc{
	ID: "trace:" + requestID + ":" + call.ToolCallID,
	SessionID: sessionID,
	Role: "tool",
	ToolName: call.ToolName,
	Content: formatToolFTSContent(call), // tool= err= args= result=
	CreatedAt: time.Now(),
}
_ = mgr.IndexMessage(ctx, sessMeta, doc)
```

- [ ] **Step 2: Idempotent re-index** same ID twice → one row.

- [ ] **Step 3: Commit if asked**

---

### Task 7: memory_recall + SessionTranscript anchored path (framework + portal adapter)

**Files:**
- Modify: `framework/memory/session_transcript.go`
- Modify: `framework/memory/session_transcript_test.go`
- Modify: `framework/tool/memory/store_tools.go`
- Modify: `framework/tool/memory/store_tools_test.go`
- Possibly extend `RecallQuery` with `AnchorWindow`, `IncludeTools`, `ExcludeCurrent` — or read only from tool params and pass via custom Search closure in portal wiring.

- [ ] **Step 1: Change empty-query behavior for transcript**

Today `SessionTranscript.Recall` returns `ErrEmptyQueryRejected` on empty query. Spec: empty + source=transcript → ListRecent.

```go
if strings.TrimSpace(q.Query) == "" {
	return s.listRecentAsHits(ctx, q)
}
// else SearchAnchored → map to MemoryHit or richer JSON via tool layer
```

- [ ] **Step 2: Tool schema**

- Remove `query` from `required` (keep `scope` required).
- Add optional `anchor_window`, `include_tools`, `exclude_current`.
- For `source=transcript` + non-empty query: return JSON:

```json
{"hits":[{"session_id":"...","title":"...","anchor":{...},"window":[...],"bookend_start":[],"bookend_end":[]}],"count":1}
```

For units/files empty query: keep rejecting or empty hits (document in description).

- [ ] **Step 3: Tests** for empty transcript query + anchored non-empty + `include_tools=false` + `exclude_current` (current session omitted).

- [ ] **Step 4: Portal TranscriptBackend wiring** — ensure GetManager path uses `SearchAnchored`.

---

### Task 8: Portal GET transcript/search API

**Files:**
- Follow existing agent/chat HTTP or gRPC pattern (prefer mirror `SearchSessions` in `chat.proto` / service if that is the house style; otherwise document hand-written `/api/v1/agents/{agent_id}/transcript/search`)
- Unify path prefix with existing gateway (`/api/v1/...` if that is current; update spec path if needed)
- Test: handler unit test with fake manager

- [ ] **Step 1:** Query params `q`, `exclude_session`, `include_tools`, `window`. AuthZ = agent read. Response `[]AnchoredHit`.

- [ ] **Step 2: Unit test required.**

---

### Task 4 note (persist hook points)

Call Persist on **SendMessage success** after `a.Run` when Metadata/trace present, and on **StreamEventDone** when `ev.Trace != nil`. Do **not** hang persist solely on `SaveAssistantMessage` without a RunTrace.

---

### Task 9: Expose in-memory messages snapshot from ReAct (framework)

**Why:** C3 needs full messages including tool roles. Today `agent.Response` / `StreamEventDone` only carry `*RunTrace`, and ChatMessage rows are not written for tools. Without this task, Task 12 cannot feed Hermes-style snapshot.

**Files:**
- Modify: `framework/agent/trace.go` (Response / StreamEvent fields)
- Modify: `framework/agent/react_agent.go` (assign snapshot before return / Done)
- Test: `framework/agent/react_agent_test.go`

- [ ] **Step 1: Failing test**

After a Run that executes one tool, `resp.Messages` (or `resp.Metadata["messages"]`) must contain assistant tool_calls + tool result messages in order.

- [ ] **Step 2: Implement**

Add to `Response`:

```go
// Messages is the in-memory conversation after this Run (may include tool roles).
// Used by Portal background review; omit from large log dumps.
Messages []model.Message `json:"-"`
```

On stream Done, set `StreamEvent.Metadata["messages"]` or add `StreamEvent.Messages`.

Populate from the agent's working `messages` slice at end of successful tool loop (copy). Cap by `growth.background_review.max_snapshot_messages` only at Portal when spawning (framework returns full copy; portal truncates with prefer-failed-tools).

- [ ] **Step 3: `go test ./agent/ -run MessagesSnapshot -count=1` — PASS**

- [ ] **Step 4: Commit if asked**

---

### Task 10: Growth model columns + synchronous FinalizeTurn (portal)

**Why:** `OnToolSuccess` currently runs via `runGrowthAsync` goroutine and may call `Wake()` before stream Done. If C3 only checks `pending` at Done, races lose spawns; if Wake is deferred and pending flips late, nothing wakes the worker.

**Files:**
- Modify: `portal/internal/data/model/growth.go`
- Modify: `portal/internal/biz/growth.go` (+ biz struct)
- Modify: `portal/internal/data/growth_mysql.go`
- Modify: `portal/internal/service/growth_chat.go`
- Modify: `portal/internal/biz/growth_test.go`
- Conf: `growth.background_review.enabled`

- [ ] **Step 1: Add columns**

`LastBackgroundReviewAt *time.Time`, `LastReviewRequestID string`, `BgReviewInFlight bool`, `BgReviewInFlightSince *time.Time`

- [ ] **Step 2: Add `FinalizeTurnForBackgroundReview`**

```go
// FinalizeTurnForBackgroundReview runs synchronously on the chat request goroutine
// after Run completes. When background_review.enabled:
// - apply toolSuccessCount / assistantTurn to counters
// - set pending_* if thresholds crossed
// - do NOT Wake()
// Returns whether skill and/or memory review should spawn in-process.
// spawn* is true if pending_* is set AFTER this call (including pending that
// already existed before this turn), not only when this turn newly crossed a threshold.
func (uc *GrowthUsecase) FinalizeTurnForBackgroundReview(ctx context.Context, sessionID, requestID string, toolSuccessCount int, assistantTurn bool) (spawnSkill, spawnMemory bool, err error)
```

`toolSuccessCount` = number of successful tools in **this** RunTrace (Portal derives from `tr.ToolCalls`). When C3 enabled, do not also run async `notifyGrowthAssistantTurn` / tool-success counter paths for the same turn (avoid double-count).

- [ ] **Step 3: When C3 enabled, change hooks**

- `growthToolSuccessHook` / assistant notify: **either** skip counter updates (moved to FinalizeTurn) **or** only bump counters without pending/Wake; **must not** double-count. Preferred: skip async counter path when C3 on; FinalizeTurn is sole counter writer for that request.
- When C3 disabled: keep existing OnToolSuccess + Wake behavior.

- [ ] **Step 4: Tests**

```go
func TestFinalizeTurn_SetsPendingWithoutWake(t *testing.T) { /* wake spy unused */ }
func TestOnToolSuccess_NoDoubleCountWhenC3Enabled(t *testing.T) { /* hooks no-op or non-pending */ }
```

- [ ] **Step 5: Commit if asked**

---

### Task 11: spawnBackgroundReview after Run/Stream Done (portal)

**Files:**
- Modify: `portal/internal/service/growth_agent_review.go`
- Modify: `portal/internal/service/chat.go` (SendMessage success + StreamEventDone)
- Create: `portal/internal/service/background_review.go`
- Tests: `background_review_test.go`

- [ ] **Step 1: Snapshot source (ordered preference)**

1. `resp.Messages` / Done event messages from Task 9  
2. Fallback: rebuild review-only messages = recent chat user/assistant from DB + synthetic `role=tool` messages from just-persisted `TurnTrace` (args/result_preview). Document fallback in code comment — not bit-identical to model context but unblocks review if snapshot missing.

- [ ] **Step 2: After PersistTurnTrace + compact boundary**

```
spawnSkill, spawnMemory, _ := growthUC.FinalizeTurnForBackgroundReview(...)
if spawnSkill || spawnMemory {
  SetBgReviewInFlight(session, true, now)
  go SpawnBackgroundReview(..., messagesSnapshot, spawnSkill, spawnMemory)
}
```

Never gate spawn solely on a pending flag written by a racing async hook.

- [ ] **Step 3: Extract** `SpawnBackgroundReview` from `spawnReviewAgent` — if `len(messages)>0`, use as conversation history; prepend skills summary system/user section. Truncate with `max_snapshot_messages` and prefer-failed-tools before fork.

- [ ] **Step 4: Child agent** — no growth success hooks / nudge.

- [ ] **Step 5: On finish** — clear in_flight; ClearGrowthPending on success; `Wake()` on failure so worker retries with TraceDigest.

- [ ] **Step 6: Tests** — FinalizeTurn true → spawn once; missing Messages → fallback synthetic tools still non-empty review input.

---

### Task 12: Worker gates + TraceDigest (portal)

**Files:**
- Modify: `portal/internal/service/growth_worker.go`
- Modify: `framework/growth/review_context.go` or portal fetch helper
- Modify: `portal/internal/biz/growth.go` (`TrySessionEnd*` dedupe — advisory but implement skip when last_review within dedupe_window and no new pending)
- Tests: worker gate unit tests

- [ ] **Step 1: Before claim**

1. If in_flight && !stale(TTL) → skip  
2. If in_flight && stale → clear in_flight, continue  
3. If within dedupe_window of last_background_review_at && no newer pending → skip  

- [ ] **Step 2: `formatTraceDigest(traces []agent.TurnTrace) string`** — failures first; append under `# Turn traces`.

- [ ] **Step 3: Config** `dedupe_window`, `in_flight_ttl`, `async_trace_turn_limit`.

- [ ] **Step 4: Tests** for stale clear + digest rendering.

---

### Task 13: Compact boundary persistence (portal)

**Files:**
- Create: `portal/internal/chat/compact_boundary.go`
- Modify: `portal/internal/service/chat.go` — call on SendMessage success path and StreamEventDone when `ev.Trace != nil` (not on SaveAssistantMessage without trace)
- Test: `compact_boundary_test.go`

- [ ] **Step 1:** If `tr.LastL2Summary != ""`:

```go
meta := map[string]any{
	"sixath.origin":  /* model.OriginCompactBoundary from framework/model */,
	"middle_removed": tr.LastL2MiddleRemoved,
	"request_id":     requestID,
}
// idempotent: skip if message with same request_id+origin exists
_, err := chatUC.CreateMessageWithMetadata(ctx, sessionID, "system", content, meta)
```

- [ ] **Step 2: Test** double call same request_id → single message.

- [ ] **Step 3: Confirm Web still renders via existing `isCompactBoundaryMessage`.**

---

### Task 14: Integration smoke (optional but recommended)

**Files:**
- Create: `portal/internal/chat/trajectory_phase1_integration_test.go` (fakes OK)

- [ ] **Step 1:** Fake RunTrace → Persist → FTS tool doc → SearchAnchored hit.  
- [ ] **Step 2:** FinalizeTurn + in_flight skips worker.  
- [ ] **Step 3:** Manual QA checklist below.

---

## Manual QA checklist

1. Enable `growth.background_review.enabled=true`, run agent with ≥10 successful tools → logs show `growth_bg_review_spawned`, pending cleared without duplicate worker run.  
2. Kill portal mid-review → after `in_flight_ttl`, worker picks up pending.  
3. `memory_recall` source=transcript with tool name query returns window including role=tool.  
4. L2 compress turn → refresh chat → compact boundary banner remains.  
5. `trace.persist.enabled=false` → no turn_traces rows.

---

## Phase 2 (not this plan)

Separate plan later: Insights API, Rewind soft-delete, optional `fork_session_on_l2`.

---

## Spec link

Add under spec §13 / §15: Plan → `docs/superpowers/plans/2026-08-05-trajectory-utilization-phase1.md`.
