# Trajectory Utilization Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.  
> `framework/` and `portal/` are nested git repos; run tests and commits inside the touched repo.  
> **Do not commit unless the user asks.**

**Goal:** Productize Phase 1 traces: Insights aggregation API (+ thin read-only UI), session Rewind with soft-hide of messages/FTS/traces, and optional L2 archive fork with parent-chain `RootSessionID` folding in `SearchAnchored`.

**Architecture:** `turn_traces` remains the authority for Insights. Rewind flips `chat_messages.active` / `turn_traces.active` and deletes matching FTS rows (including `trace:` tool projections) so ListMessages and recall stay consistent. Fork-on-L2 (off by default) creates a child session with `parent_session_id`; SearchAnchored uses existing `rootSessionID` walk (today hard-coded `RootSessionID == SessionID`).

**Tech Stack:** Go; Portal GORM MySQL; `framework/sessionsearch` SQLite FTS5; optional thin React page under `web/`.

**Spec:** [docs/superpowers/specs/2026-08-05-trajectory-utilization-design.md](../specs/2026-08-05-trajectory-utilization-design.md) §2.2 G6–G8, §8–§10, §12 二期, §13 P2-A…P2-C.

**Depends on:** Phase 1 shipped (`turn_traces`, FTS tool projections, `SearchAnchored`, compact boundary).  
**Out of scope:** ShareGPT/RL (G9), achievements, token/cost Insights fields, rewriting L0/L1/L2 algorithms.

**Locked implementation choices (fill design “或”):**

| Topic | Choice |
|-------|--------|
| Message soft-hide | `chat_messages.active` bool (default true), mirror `turn_traces.active` — **not** `deleted_at` |
| Session rewind counter | `chat_sessions.rewind_count` int, increment on each successful Rewind |
| Insights UI | Hand-written HTTP API + minimal Agent Insights page (read-only table); no proto regen required |
| fork_session | Env stand-in `SATH_COMPACT_FORK_SESSION` (default false); optional later YAML `compact.fork_session_on_l2` |
| FTS on Rewind | Hard-delete FTS message rows for deactivated chat message IDs + `trace:{requestID}:*` for deactivated traces |

---

## File map

| File | Responsibility |
|------|----------------|
| Modify `portal/internal/data/model/chat.go` | `ChatMessage.Active`; `ChatSession.RewindCount` |
| Modify `portal/internal/data/chat_mysql.go` | ListBySession filters `active=true`; SoftDeactivateAfter; GetMessage |
| Modify `portal/internal/biz/chat.go` | `RewindToMessage`; repo methods |
| Modify `portal/internal/data/turn_trace_mysql.go` | `DeactivateAfter(sessionID, createdAt)` / by request set |
| Modify `framework/sessionsearch/index.go` + manager | `RemoveMessages(ids)`; SearchAnchored `RootSessionID = rootSessionID(...)` |
| Create `portal/internal/service/insights.go` | Aggregate from TurnTraceStore |
| Create `portal/internal/server/insights.go` | `GET /api/v1/agents/{agent_id}/insights` |
| Create `portal/internal/server/rewind.go` | `POST /api/v1/sessions/{session_id}/rewind` |
| Modify `portal/internal/service/chat.go` | Optional fork on L2 after compact boundary; cancel stream before rewind |
| Modify `portal/internal/chat/compact_boundary.go` or new `fork_on_compact.go` | Create child session + parent link when flag on |
| Create/Modify `web/src/api/client.ts` + Insights page | Read-only Insights; Rewind button on chat (optional Task) |
| Tests | `*_test.go` per task below |

---

### Task 1: Schema — message `active` + session `rewind_count` (portal)

**Files:**
- Modify: `portal/internal/data/model/chat.go`
- Modify: `portal/internal/data/data.go` (AutoMigrate already covers models)
- Modify: `portal/internal/data/chat_mysql.go`
- Test: `portal/internal/data/chat_rewind_mysql_test.go`

- [x] **Step 1: Failing test**

```go
func TestListBySession_SkipsInactive(t *testing.T) {
	// insert 3 messages; SoftDeactivate id>=mid; List returns only earlier actives
}
```

- [x] **Step 2: Add fields**
- [x] **Step 3: Repo helpers**
- [x] **Step 4: `go test ./internal/data/ -run Rewind -count=1` — PASS**
- [ ] **Step 5: Commit if asked**

---

### Task 2: TurnTrace deactivate (portal)

**Files:**
- Modify: `portal/internal/data/turn_trace_mysql.go`
- Modify: `framework/turntrace/store.go` (optional method on interface)
- Test: `portal/internal/data/turn_trace_mysql_test.go`

- [ ] **Step 1: Failing test** — Upsert two traces with different CreatedAt; DeactivateAfter mid time → ListBySession only earlier.

- [ ] **Step 2: Implement**

```go
func (s *TurnTraceStore) DeactivateAfter(ctx context.Context, sessionID string, at time.Time) (requestIDs []string, err error)
```

Sets `active=false` where `session_id=? AND created_at >= ?` (use `>=` so same-second traces after rewind hide). List/Get for Insights/digest keep `active=true` filter (already on ListBySession).

- [ ] **Step 3: Tests PASS**

---

### Task 3: FTS RemoveMessages (framework)

**Files:**
- Modify: `framework/sessionsearch/types.go` (interface)
- Modify: `framework/sessionsearch/index.go`, `manager.go`
- Test: `framework/sessionsearch/remove_messages_test.go`

- [ ] **Step 1: Failing test** — Index 3 docs; RemoveMessages([id2]); Search no longer hits id2 content.

- [ ] **Step 2: Implement**

```go
func (m *IndexManager) RemoveMessages(ctx context.Context, messageIDs []string) error
```

Delete `messages_fts` then `messages` for each id (reuse RemoveSession loop body). No-op on empty.

Also helper used by Rewind for tool projections:

```go
// RemoveTraceProjections deletes docs with id LIKE "trace:{requestID}:%"
func (m *IndexManager) RemoveTraceProjections(ctx context.Context, sessionID, requestID string) error
```

- [ ] **Step 3: `go test ./sessionsearch/ -run Remove -count=1` — PASS**

---

### Task 4: Rewind usecase + HTTP (portal)

**Files:**
- Modify: `portal/internal/biz/chat.go`
- Create: `portal/internal/service/rewind.go` (or methods on ChatService)
- Create: `portal/internal/server/rewind.go`
- Modify: `portal/internal/server/http.go`
- Test: `portal/internal/biz/chat_rewind_test.go`

- [ ] **Step 1: API**

`POST /api/v1/sessions/{session_id}/rewind`  
Body: `{ "message_id": "<uuid>" }`  
AuthZ: same as session write.

- [ ] **Step 2: Usecase flow**

1. Load message; must belong to session; must be `active`.  
2. If session has in-flight stream: cancel / reject with `409 CONFLICT` if cancel unavailable — prefer cancel when ChatService exposes cancel hook; else document “client must stop stream first”.  
3. `SoftDeactivateAfter` → message IDs.  
4. `TurnTraceStore.DeactivateAfter(sessionID, anchor.CreatedAt)` → request IDs.  
5. FTS: `RemoveMessages` for chat IDs; `RemoveTraceProjections` per request ID.  
6. `BumpRewindCount`.  
7. Return `{ session_id, rewind_count, deactivated_messages, deactivated_traces }`.

Do **not** roll back Skills (spec).

- [ ] **Step 3: Unit test with fake repos** — after rewind, ListMessages shorter; inactive not returned.

- [ ] **Step 4: Wire route next to transcript/search.**

- [ ] **Step 5: Commit if asked**

---

### Task 5: Insights aggregation + HTTP (portal) — P2-A

**Files:**
- Create: `portal/internal/service/insights.go`
- Create: `portal/internal/server/insights.go`
- Modify: `portal/internal/server/http.go`
- Modify: `portal/internal/data/turn_trace_mysql.go` — `ListByAgent(ctx, agentID, from, to, limit)` if missing
- Test: `portal/internal/service/insights_test.go`

- [ ] **Step 1: Response shape**

```json
{
  "agent_id": "...",
  "from": "...",
  "to": "...",
  "turns": 12,
  "tool_calls": 40,
  "error_calls": 3,
  "error_rate": 0.075,
  "blocked_calls": 1,
  "top_tools": [{"name":"terminal","calls":10,"errors":1}],
  "top_sessions": [{"session_id":"...","turns":4,"errors":2}]
}
```

Source: `turn_traces` with `active=true`, filter `agent_id` + `created_at` range. Parse `payload_json` Calls for errors / Blocked. Cap scan (e.g. 5000 rows) with warning field if truncated.

- [ ] **Step 2: Route** `GET /api/v1/agents/{agent_id}/insights?from=&to=` (RFC3339 or unix; default last 7d).

- [ ] **Step 3: Tests** — synthetic traces → expected top_tools / error_rate.

- [ ] **Step 4: Optional thin UI** — `web/src/pages/AgentInsightsPage.tsx` + client method; skip if time-boxed (API alone meets G6 “只读 API”; UI is “只读页” — do UI in same task if web build is cheap).

---

### Task 6: SearchAnchored parent fold (framework) — part of G8

**Files:**
- Modify: `framework/sessionsearch/index.go` (`SearchAnchored`)
- Test: `framework/sessionsearch/anchored_test.go`

- [ ] **Step 1: Failing test** — sessions `child.parent=parent`; index hit on child; `RootSessionID` must be parent root (walk via `rootSessionID`).

- [ ] **Step 2: Replace**

```go
RootSessionID: rootSessionID(sessions, sid),
```

Update comment (remove “phase 1: no parent fold”). Keep message-level hits (do **not** collapseHits).

- [ ] **Step 3: Tests PASS**

---

### Task 7: Optional fork_session on L2 (portal) — P2-C

**Files:**
- Create: `portal/internal/chat/fork_on_compact.go`
- Modify: `portal/internal/service/chat.go` (after PersistCompactBoundary)
- Modify: `portal/internal/biz/chat.go` if CreateSession needs “readonly parent” flag
- Test: `portal/internal/chat/fork_on_compact_test.go`

- [ ] **Step 1: Config**

```go
func CompactForkSessionEnabled() bool {
  // SATH_COMPACT_FORK_SESSION true/1/yes → on; default false
}
```

- [ ] **Step 2: When enabled and L2 boundary just persisted**

1. Create new session: `parent_session_id=old`, title `archived: {old title}` or keep title.  
2. Mark old session readonly: add `chat_sessions.readonly` bool **or** encode in metadata/title — **prefer** `Readonly bool` column default false; SendMessage rejects if readonly.  
3. Seed new session with system message = LastL2Summary (or copy compact_boundary content).  
4. Return/log new session_id for client switch (Stream Done metadata `forked_session_id` if stream path).

Default **off** preserves Phase 1 same-session boundary behavior (策略 B).

- [ ] **Step 3: Tests** — flag off → no fork; flag on → parent set + old readonly.

---

### Task 8: Web Rewind control (optional, recommended)

**Files:**
- Modify: `web/src/api/client.ts` — `rewindSession(sessionId, messageId)`
- Modify: `web/src/pages/ChatPage.tsx` — context menu / button “从此处重新开始”

- [x] **Step 1:** Call API; on success reload messages from ListMessages.  
- [x] **Step 2:** Manual checklist item below.

---

### Task 9: Integration smoke

**Files:**
- Create: `portal/internal/service/trajectory_phase2_integration_test.go` (fakes OK)

- [x] **Step 1:** Fake traces → Insights top_tools (unit: `TestAggregateInsights_*`).  
- [x] **Step 2:** Rewind → ListMessages shorter (unit + live `_neo4j_q/verify_trajectory_phase2.ps1`).  
- [x] **Step 3:** SearchAnchored RootSessionID with parent chain (unit).  
- [x] **Step 4:** Manual QA — live smoke `SMOKE_OK` (insights + rewind).

---

## Manual QA checklist

1. Agent with tool traffic → Insights shows non-zero turns/tools; error_rate matches a known failed tool.  
2. Rewind to mid-chat message → UI loses later messages; `memory_recall` / transcript/search no longer returns deactivated tool projections; new user message continues from remaining history.  
3. `SATH_COMPACT_FORK_SESSION=false` → L2 still same-session compact banner (Phase 1).  
4. `SATH_COMPACT_FORK_SESSION=true` → after L2, new session with parent; old rejects SendMessage; SearchAnchored on child sets RootSessionID to root.  
5. Rewind does not delete workspace Skills.

---

## Suggested order

| Order | Task | Goal |
|-------|------|------|
| 1 | Task 1–2 | Schema soft-hide |
| 2 | Task 3–4 | Rewind end-to-end (G7) |
| 3 | Task 5 | Insights (G6) |
| 4 | Task 6–7 | Parent fold + optional fork (G8) |
| 5 | Task 8–9 | UI + smoke |

Insights (G6) can parallel Rewind after Task 2 (`ListByAgent`).

---

## Spec link

Update design §13: Plan (二期) → `docs/superpowers/plans/2026-08-06-trajectory-utilization-phase2.md`.
