# P3-A FailureSignal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.  
> `framework/` and `portal/` are nested git repos; run tests and commits inside the touched repo.  
> **Do not commit unless the user asks.**

**Goal:** Emit structured `FailureSignal` from at least two live event sources (`ToolFailed`, `ToolGuardrailWarn`), with identity from context, a recordable sink, and structured logs — **no** MemoryStore / procedural commit.

**Architecture:** Map existing `events.Bus` payloads → `memory.FailureSignal` via a sync subscriber (`AttachFailureSignalBridge`). Portal attaches the bridge to each chat **`turnBus`** (not process `DefaultBus`). A `FailureSignalSink` receives signals (recording sink for tests; logging + ring buffer in Portal). Orthogonal to G4 `FailureCaptureHook` (ERRORS.md).

**Tech Stack:** Go; `framework/events`; `framework/tool` context keys; Portal per-turn `turnBus` in `internal/service/chat.go`.

**Spec:** `docs/superpowers/specs/2026-07-30-procedural-repair-harness-design.md` §4 (P3-A only).  
**Pilot note:** `zone-4100-agent` is reserved for P3-E enablement; P3-A emits/logs for all agents on the streaming chat path (side-effect-free).

---

## File map

| File | Responsibility |
|------|----------------|
| Create `framework/memory/failure_signal.go` | `FailureSignal`, code constants, `FailureSignalFromEvent`, identity helpers |
| Create `framework/memory/failure_signal_sink.go` | `FailureSignalSink`, `RecordingFailureSink`, `LoggingFailureSink`, `MultiFailureSink`, `AttachFailureSignalBridge` |
| Create `framework/memory/failure_signal_test.go` | Mapper + bridge + sink tests |
| Create `portal/internal/chat/failure_signal_sink.go` | Shared default sink factory: Logging + Ring(64) |
| Modify `portal/internal/service/chat.go` | After `turnBus := events.NewBus()` (~L495), `memory.AttachFailureSignalBridge(turnBus, chat.DefaultFailureSignalSink())` |
| Test: extend or add under `portal/internal/service/` | turnBus + RecordingFailureSink receives ToolFailed (unit-level, no full SSE) |
| Modify `portal/docs/memory-integration.md` | Short P3-A section |
| Modify umbrella spec §13 | Plan link (already added at plan-write time) |

**Out of scope (do not implement in this plan):**

- `memory_units.kind` migration / procedural write  
- Episode local buffer (P3-B)  
- Skill router / Prefetch lane (P3-C/E)  
- `task_failed` evaluator codes  
- Changing G4 `FailureCaptureHook` behavior  
- Prefer/auto_commit / pilot_agents gating (P3-E)

---

### Task 1: FailureSignal type + event mapper (framework)

**Files:**
- Create: `framework/memory/failure_signal.go`
- Test: `framework/memory/failure_signal_test.go`

- [ ] **Step 1: Write failing tests for mapper**

```go
package memory

import (
	"context"
	"testing"
	"time"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/tool"
)

func TestFailureSignalFromEvent_ToolFailed(t *testing.T) {
	ctx := context.WithValue(context.Background(), tool.ContextKeyAgentID, "zone-4100-agent")
	ctx = context.WithValue(ctx, tool.ContextKeySessionID, "sess-1")
	e := events.Event{
		Kind: events.ToolFailed,
		Payload: map[string]any{
			"tool":  "ssh_exec",
			"error": "exit 1",
			"step":  3,
		},
		At: time.Unix(100, 0).UTC(),
	}
	sig, ok := FailureSignalFromEvent(ctx, e)
	if !ok {
		t.Fatal("expected signal")
	}
	if sig.Code != FailureCodeToolFailed {
		t.Fatalf("code: %s", sig.Code)
	}
	if sig.AgentID != "zone-4100-agent" || sig.SessionID != "sess-1" {
		t.Fatalf("identity: %#v", sig)
	}
	if sig.TaskFamily != "zone-4100-agent" {
		t.Fatalf("task family default: %q", sig.TaskFamily)
	}
	if sig.ToolName != "ssh_exec" || sig.Evidence["error"] == "" {
		t.Fatalf("tool/evidence: %#v", sig)
	}
}

func TestFailureSignalFromEvent_ToolGuardrailWarn(t *testing.T) {
	e := events.Event{
		Kind: events.ToolGuardrailWarn,
		Payload: map[string]any{
			"rule":   "same_tool_failure",
			"tool":   "ssh_exec",
			"streak": 3,
		},
	}
	sig, ok := FailureSignalFromEvent(context.Background(), e)
	if !ok || sig.Code != FailureCodeToolRepeatFail {
		t.Fatalf("got ok=%v sig=%#v", ok, sig)
	}
	if sig.Evidence["rule"] != "same_tool_failure" {
		t.Fatalf("evidence: %#v", sig.Evidence)
	}
}

func TestFailureSignalFromEvent_IgnoresUnrelated(t *testing.T) {
	_, ok := FailureSignalFromEvent(context.Background(), events.Event{Kind: events.ToolExecuted})
	if ok {
		t.Fatal("must ignore")
	}
}

func TestFailureSignalFromEvent_PermissionDenied(t *testing.T) {
	e := events.Event{
		Kind:    events.PermissionDenied,
		Payload: map[string]any{"tool": "execute_write", "reason": "denied"},
	}
	sig, ok := FailureSignalFromEvent(context.Background(), e)
	if !ok || sig.Code != FailureCodePolicyViolation {
		t.Fatalf("got ok=%v sig=%#v", ok, sig)
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL (undefined symbols)**

```bash
cd framework
go test ./memory/ -run TestFailureSignalFromEvent -count=1
```

Expected: compile error / undefined `FailureSignalFromEvent`.

- [ ] **Step 3: Implement minimal types + mapper**

In `framework/memory/failure_signal.go`:

```go
package memory

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/tool"
)

const (
	FailureCodeToolFailed       = "tool_failed"
	FailureCodeToolRepeatFail   = "tool_repeat_fail"
	FailureCodePolicyViolation  = "policy_violation"
	// FailureCodeUserReject reserved; not mapped in P3-A unless a clear Bus event exists.
	FailureCodeUserReject = "user_reject"
	FailureCodeTaskFailed = "task_failed" // reserved; no emitter in P3-A
)

type FailureSignal struct {
	Code       string
	AgentID    string
	SessionID  string
	TaskFamily string
	ToolName   string
	SkillID    string
	Message    string
	Evidence   map[string]string
	At         time.Time
}

// FailureSignalFromEvent maps Bus events to FailureSignal.
// Returns ok=false for unrelated kinds or empty/unusable payloads.
func FailureSignalFromEvent(ctx context.Context, e events.Event) (FailureSignal, bool) {
	switch e.Kind {
	case events.ToolFailed:
		return mapToolFailed(ctx, e)
	case events.ToolGuardrailWarn:
		return mapGuardrailWarn(ctx, e)
	case events.PermissionDenied:
		return mapPermissionDenied(ctx, e)
	default:
		return FailureSignal{}, false
	}
}

func identityFromCtx(ctx context.Context) (agentID, sessionID, family string) {
	if ctx != nil {
		agentID, _ = ctx.Value(tool.ContextKeyAgentID).(string)
		sessionID, _ = ctx.Value(tool.ContextKeySessionID).(string)
	}
	agentID = strings.TrimSpace(agentID)
	sessionID = strings.TrimSpace(sessionID)
	// P3-A: TaskFamily = agent_id; Agent tag enrichment is P3-C §6.5.
	family = agentID
	return agentID, sessionID, family
}

func payloadString(p map[string]any, key string) string {
	if p == nil {
		return ""
	}
	v, ok := p[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatInt(int64(t), 10)
	default:
		return fmt.Sprint(t)
	}
}

func mapToolFailed(ctx context.Context, e events.Event) (FailureSignal, bool) {
	toolName := payloadString(e.Payload, "tool")
	errMsg := payloadString(e.Payload, "error")
	if toolName == "" && errMsg == "" {
		return FailureSignal{}, false
	}
	agentID, sessionID, family := identityFromCtx(ctx)
	ev := map[string]string{}
	if errMsg != "" {
		ev["error"] = errMsg
	}
	if s := payloadString(e.Payload, "step"); s != "" {
		ev["step"] = s
	}
	if s := payloadString(e.Payload, "tool_call_id"); s != "" {
		ev["tool_call_id"] = s
	}
	at := e.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return FailureSignal{
		Code:       FailureCodeToolFailed,
		AgentID:    agentID,
		SessionID:  sessionID,
		TaskFamily: family,
		ToolName:   toolName,
		Message:    errMsg,
		Evidence:   ev,
		At:         at,
	}, true
}

func mapGuardrailWarn(ctx context.Context, e events.Event) (FailureSignal, bool) {
	rule := payloadString(e.Payload, "rule")
	toolName := payloadString(e.Payload, "tool")
	if rule == "" {
		return FailureSignal{}, false
	}
	agentID, sessionID, family := identityFromCtx(ctx)
	ev := map[string]string{"rule": rule}
	for _, k := range []string{"streak", "threshold_warn", "threshold_halt", "stable_args_key"} {
		if s := payloadString(e.Payload, k); s != "" {
			ev[k] = s
		}
	}
	at := e.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	msg := "tool_guardrail:" + rule
	return FailureSignal{
		Code:       FailureCodeToolRepeatFail,
		AgentID:    agentID,
		SessionID:  sessionID,
		TaskFamily: family,
		ToolName:   toolName,
		Message:    msg,
		Evidence:   ev,
		At:         at,
	}, true
}

func mapPermissionDenied(ctx context.Context, e events.Event) (FailureSignal, bool) {
	toolName := payloadString(e.Payload, "tool")
	reason := payloadString(e.Payload, "reason")
	if toolName == "" && reason == "" {
		return FailureSignal{}, false
	}
	agentID, sessionID, family := identityFromCtx(ctx)
	ev := map[string]string{}
	if reason != "" {
		ev["reason"] = reason
	}
	at := e.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return FailureSignal{
		Code:       FailureCodePolicyViolation,
		AgentID:    agentID,
		SessionID:  sessionID,
		TaskFamily: family,
		ToolName:   toolName,
		Message:    reason,
		Evidence:   ev,
		At:         at,
	}, true
}
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
cd framework
go test ./memory/ -run TestFailureSignalFromEvent -count=1
```

Expected: `PASS`

- [ ] **Step 5: Commit only if user asked** (otherwise skip)

```bash
cd framework
git add memory/failure_signal.go memory/failure_signal_test.go
# git commit -m "feat(memory): map Bus events to FailureSignal (P3-A)"
```

---

### Task 2: Sink + Bus bridge (framework)

**Files:**
- Create: `framework/memory/failure_signal_sink.go`
- Modify: `framework/memory/failure_signal_test.go`

- [ ] **Step 1: Write failing bridge tests**

```go
func TestAttachFailureSignalBridge_RecordsToolFailedAndGuardrail(t *testing.T) {
	bus := events.NewBus()
	rec := &RecordingFailureSink{}
	AttachFailureSignalBridge(bus, rec)

	ctx := context.WithValue(context.Background(), tool.ContextKeyAgentID, "a1")
	ctx = context.WithValue(ctx, tool.ContextKeySessionID, "s1")
	bus.Publish(ctx, events.Event{
		Kind:    events.ToolFailed,
		Payload: map[string]any{"tool": "t", "error": "e"},
	})
	bus.Publish(ctx, events.Event{
		Kind:    events.ToolGuardrailWarn,
		Payload: map[string]any{"rule": "same_tool_failure", "tool": "t", "streak": 2},
	})
	bus.Publish(ctx, events.Event{Kind: events.ToolExecuted, Payload: map[string]any{"tool": "t"}})

	got := rec.Snapshot()
	if len(got) != 2 {
		t.Fatalf("want 2 signals, got %#v", got)
	}
	if got[0].Code != FailureCodeToolFailed || got[1].Code != FailureCodeToolRepeatFail {
		t.Fatalf("codes: %#v", got)
	}
}

func TestAttachFailureSignalBridge_NilSafe(t *testing.T) {
	AttachFailureSignalBridge(nil, &RecordingFailureSink{})
	AttachFailureSignalBridge(events.NewBus(), nil)
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
cd framework
go test ./memory/ -run TestAttachFailureSignalBridge -count=1
```

- [ ] **Step 3: Implement sink + bridge**

```go
package memory

import (
	"context"
	"log/slog"
	"sync"

	"github.com/sixath/framework/events"
)

type FailureSignalSink interface {
	OnFailureSignal(ctx context.Context, sig FailureSignal)
}

type RecordingFailureSink struct {
	mu   sync.Mutex
	sigs []FailureSignal
}

func (r *RecordingFailureSink) OnFailureSignal(_ context.Context, sig FailureSignal) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sigs = append(r.sigs, sig)
}

func (r *RecordingFailureSink) Snapshot() []FailureSignal {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]FailureSignal, len(r.sigs))
	copy(out, r.sigs)
	return out
}

// LoggingFailureSink logs structured fields (code, agent_id, tool_name, …).
type LoggingFailureSink struct {
	Logger *slog.Logger // nil → slog.Default()
}

func (l LoggingFailureSink) OnFailureSignal(_ context.Context, sig FailureSignal) {
	logger := l.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("failure_signal",
		"code", sig.Code,
		"agent_id", sig.AgentID,
		"session_id", sig.SessionID,
		"task_family", sig.TaskFamily,
		"tool_name", sig.ToolName,
		"skill_id", sig.SkillID,
		"message", sig.Message,
		"evidence", sig.Evidence,
		"at", sig.At,
	)
}

type MultiFailureSink []FailureSignalSink

func (m MultiFailureSink) OnFailureSignal(ctx context.Context, sig FailureSignal) {
	for _, s := range m {
		if s != nil {
			s.OnFailureSignal(ctx, sig)
		}
	}
}

// AttachFailureSignalBridge registers a sync subscriber on bus.
// Safe with nil bus or nil sink (no-op). Multiple attaches = multiple subscribers (Portal must once).
func AttachFailureSignalBridge(bus *events.Bus, sink FailureSignalSink) {
	if bus == nil || sink == nil {
		return
	}
	bus.Subscribe(false, func(ctx context.Context, e events.Event) {
		sig, ok := FailureSignalFromEvent(ctx, e)
		if !ok {
			return
		}
		sink.OnFailureSignal(ctx, sig)
	})
}
```

Optional ring buffer was promoted to **required** (used by Portal `DefaultFailureSignalSink`):

```go
// RingFailureSink keeps the last N signals for debugging / future P3-E evidence lookup.
type RingFailureSink struct {
	N    int
	mu   sync.Mutex
	ring []FailureSignal
}
```

Implement `OnFailureSignal` + `Snapshot` as previously sketched (mutex + trim to N).

- [ ] **Step 4: Run — expect PASS**

```bash
cd framework
go test ./memory/ -run 'TestFailureSignal|TestAttachFailureSignal' -count=1
```

- [ ] **Step 5: Commit only if user asked**

---

### Task 3: Portal wire on per-turn `turnBus` (not DefaultBus)

**Why not DefaultBus:** `portal/internal/service/chat.go` creates a private `turnBus := events.NewBus()` per `SendMessageStream`, then `WithReActEventBus(turnBus)` **overrides** `BuildReActAgent`'s default `DefaultBus`. Comments explicitly avoid `SetDefaultBus`. Attaching only to `DefaultBus` / `sync.Once` at process start will miss live ToolFailed / ToolGuardrailWarn on the main chat path.

**Files:**
- Create: `portal/internal/chat/failure_signal_sink.go` — default Logging+Ring sink (process-shared OK)
- Modify: `portal/internal/service/chat.go` — attach bridge to **this turn's** `turnBus` immediately after construction (~L495), alongside the existing model-call Subscribe
- Create: `portal/internal/service/failure_signal_turnbus_test.go` (or chat package test helper)

- [ ] **Step 1: Confirm wire site**

```bash
cd portal
rg -n "turnBus := events.NewBus" --glob "*.go"
```

Expected: `internal/service/chat.go` (~495). Read surrounding Subscribe block so the new attach sits **before** ReAct run starts.

- [ ] **Step 2: Write failing test — attach to a local bus (mirrors turnBus)**

```go
package service

import (
	"context"
	"testing"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/tool"
)

func TestAttachFailureSignalOnTurnBus(t *testing.T) {
	turnBus := events.NewBus()
	rec := &memory.RecordingFailureSink{}
	memory.AttachFailureSignalBridge(turnBus, rec)

	ctx := context.WithValue(context.Background(), tool.ContextKeyAgentID, "zone-4100-agent")
	ctx = context.WithValue(ctx, tool.ContextKeySessionID, "sess-1")
	turnBus.Publish(ctx, events.Event{
		Kind:    events.ToolFailed,
		Payload: map[string]any{"tool": "ssh_exec", "error": "boom"},
	})
	got := rec.Snapshot()
	if len(got) != 1 || got[0].Code != memory.FailureCodeToolFailed {
		t.Fatalf("got %#v", got)
	}
}
```

(If `service` package import cycle blocks using chat helpers, keep this test in `framework/memory` as already covered by Task 2, and add a **compile-checked** comment + tiny helper test in `chat` for `DefaultFailureSignalSink()` non-nil.)

- [ ] **Step 3: Implement default sink + chat.go attach**

`portal/internal/chat/failure_signal_sink.go`:

```go
package chat

import (
	"sync"

	"github.com/sixath/framework/memory"
)

var (
	defaultFailureSinkOnce sync.Once
	defaultFailureSink     memory.FailureSignalSink
)

// DefaultFailureSignalSink returns a process-shared Logging+Ring sink.
// Safe to pass into AttachFailureSignalBridge on every turnBus.
func DefaultFailureSignalSink() memory.FailureSignalSink {
	defaultFailureSinkOnce.Do(func() {
		defaultFailureSink = memory.MultiFailureSink{
			memory.LoggingFailureSink{},
			&memory.RingFailureSink{N: 64},
		}
	})
	return defaultFailureSink
}
```

In `portal/internal/service/chat.go` immediately after `turnBus := events.NewBus()`:

```go
turnBus := events.NewBus()
memory.AttachFailureSignalBridge(turnBus, chat.DefaultFailureSignalSink())
```

Add imports for `memory` and ensure `chat` package import path is correct (`…/portal/internal/chat`).

**Do not** use `events.DefaultBus()` or process-level `Ensure…Once` that returns early when DefaultBus is nil.

- [ ] **Step 4: Verify agent run ctx carries identity**

```bash
cd portal
rg -n "ContextKeyAgentID|ContextKeySessionID" internal/service/chat.go internal/chat --glob "*.go"
```

If `Publish` ctx during tool execution already has these keys (via ReAct / tool registry), no change. If missing, add a **minimal** follow-up in the same PR: set keys on the ctx passed into `Run`/`RunStream` (do not invent a second identity channel). Document finding in the PR/notes.

- [ ] **Step 5: Run tests**

```bash
cd portal
go test ./internal/chat/ ./internal/service/ -count=1
```

```bash
cd framework
go test ./memory/ ./agent/ -run 'TestFailureSignal|TestAttachFailureSignal|TestDesignSection6_4' -count=1
```

Expected: PASS (ignore unrelated pre-existing failures).

- [ ] **Step 6: Commit only if user asked**

---

### Task 4: Docs + umbrella link

**Files:**
- Modify: `portal/docs/memory-integration.md`
- Modify: `docs/superpowers/specs/2026-07-30-procedural-repair-harness-design.md` §13 (plan path — already linked when plan was written; on delivery add “P3-A delivered” note)

- [ ] **Step 1: Add P3-A section to memory-integration.md** (short)

Content bullets:

- What: per-turn `turnBus` → `FailureSignal` (`tool_failed`, `tool_repeat_fail`, `policy_violation`)
- Not: no write to `memory_units`; not G4 ERRORS.md; not global DefaultBus
- Fields logged: `code`, `agent_id`, `tool_name`, …
- Next: P3-B episode boundary

- [ ] **Step 2: Spec revision** — if not already present, add plan path under §13

- [ ] **Step 3: Full regression commands**

```bash
cd framework
go test ./memory/ ./events/ ./agent/ -count=1

cd ../portal
go test ./internal/chat/ ./internal/service/ -count=1
```

Expected: all PASS (or pre-existing failures unrelated — do not expand scope to fix unless caused by this change).

---

## Acceptance checklist (spec §4.4)

| Criterion | How verified |
|-----------|----------------|
| ≥2 sources emit `Code` | Unit tests: ToolFailed + ToolGuardrailWarn (+ PermissionDenied bonus) |
| No FailureSignal ⇒ no procedural commit | N/A commit path; bridge does not call MemoryStore |
| Structured log/metrics fields | `LoggingFailureSink` keys; test Snapshot fields |
| Live Portal path | Bridge on `turnBus` in `chat.go`, not DefaultBus |

---

## Risk notes for implementers

1. **Wrong bus:** Never attach only to `DefaultBus` for Portal chat — main path uses per-turn `turnBus`.  
2. **Multiple attaches:** Calling `AttachFailureSignalBridge` every turn adds one subscriber per turnBus instance (OK — bus is discarded after turn). Shared sink must be concurrency-safe (Recording/Ring already mutex).  
3. **Missing identity:** If Portal does not set `tool.ContextKeyAgentID` / `SessionID` on the ctx used by `Publish`, AgentID/SessionID will be empty — verify and fix minimally (Task 3 Step 4).  
4. **Do not** treat model self-critique text as a FailureSignal source.  
5. Keep G4 hook unchanged; both can coexist.

---

## After P3-A

Next plan: **P3-B** episode-local vs global write boundary (umbrella §5).
