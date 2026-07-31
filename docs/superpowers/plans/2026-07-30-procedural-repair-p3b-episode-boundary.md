# P3-B Episode Write Boundary Implementation Plan

> **For agentic workers:** Use TDD. Nested repos: `framework/`, `portal/`. **Do not commit unless asked.**  
> Continue on branch `feat/p3a-failure-signal` (or rename); builds on P3-A.

**Goal:** Hard-separate episode-local failure/retry scratch from MemoryStore; block accidental `kind=procedural` Remember until P3-E; stamp turn extract as `kind=fact`.

**Architecture:** `EpisodeLocalBuffer` (session-keyed, Clear on turn end) receives FailureSignals via MultiSink on `turnBus`. Facade rejects `metadata.kind=procedural`. Extract always sets `kind=fact`.

**Spec:** umbrella §5. **Stack:** Go, existing P3-A bridge.

---

## Files

| File | Change |
|------|--------|
| Create `framework/memory/episode_local.go` | Buffer + FailureSignal sink adapter |
| Create `framework/memory/episode_local_test.go` | Clear / isolation tests |
| Modify `framework/memory/store.go` | `ErrProceduralRememberBlocked` |
| Modify `framework/memory/facade.go` | Reject procedural Remember |
| Create/modify facade test | procedural blocked |
| Modify `framework/memory/turn_extract.go` | `meta["kind"]="fact"` |
| Modify `framework/memory/turn_extract_test.go` | assert kind=fact on remember |
| Modify `portal/internal/service/chat.go` | per-turn buffer + MultiSink + Clear |
| Modify `portal/docs/memory-integration.md` | 本轮 vs 全局 |
| Modify umbrella §13 | P3-B delivered |

## Tasks

1. EpisodeLocalBuffer + tests (Clear → empty; signals not on MemoryStore)
2. Facade reject `kind=procedural` + extract stamps `kind=fact`
3. Portal turnBus MultiSink + defer Clear
4. Docs + `go test`

## Out of scope

kind column migration, procedural commit, Prefetch lane, Skill router (P3-C/E).
