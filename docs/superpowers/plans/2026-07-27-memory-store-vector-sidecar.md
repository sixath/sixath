# MemoryStore P2-E Units Vector Sidecar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** SQLite units VectorIndex + Facade hybrid Recall/peer + Portal Embedder wiring (`memory_vector`).

**Spec:** `docs/superpowers/specs/2026-07-27-memory-store-vector-sidecar-design.md`

**Repos:** `framework/`、`portal/` 嵌套 git 分别 commit。

---

### Task 1: VectorIndex sqlite

- Create: `framework/memory/vector_index.go`（接口类型）
- Create: `framework/memory/sqlite_vector_index.go`
- Test: `framework/memory/sqlite_vector_index_test.go`

### Task 2: Facade wire

- Modify: `framework/memory/facade.go`
- Test: `framework/memory/facade_vector_test.go`

### Task 3: Portal config + wiring

- Modify: `framework/config/tool_guardrails.go` + tests
- Create: `portal/internal/chat/memory_vector.go` + tests
- Modify: `portal/internal/chat/memory_store.go` / conflict options

### Task 4: Docs

- `portal/docs/memory-integration.md`
- Spec status → 已交付 when done
