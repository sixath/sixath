# MemoryStore P2-F Prefetch Quota Implementation Plan

> **For agentic workers:** Use TDD. `framework/` and `portal/` are nested git repos.

**Goal:** Global `max_total` + content-hash dedupe on `StorePrefetchBackend` after user→session→agent merge.

**Spec:** `docs/superpowers/specs/2026-07-27-memory-store-prefetch-quota-design.md`

## Files

| File | Change |
|------|--------|
| `framework/memory/store_prefetch_backend.go` | `MaxTotal`; dedupe+cap helper |
| `framework/memory/store_prefetch_backend_test.go` | dedupe / cap / max_total<=0 |
| `framework/config/tool_guardrails.go` | `MaxSnippets`, `MaxTotal` on `MemoryOrchestratorPrefetch` |
| `framework/config/config_test.go` | YAML parse |
| `portal/internal/chat/memory_prefetch_bootstrap.go` | wire fields |
| `portal/configs/agent_extra.yaml` | comments |
| `portal/docs/memory-integration.md` | P2-F + backlog |
| monorepo facade §8.6 | link P2-F |

## Tasks

1. Failing tests for dedupe + max_total
2. Implement `applyPrefetchQuota`
3. Config + Portal wire + docs
4. `go test ./memory/ ./config/` and portal chat package
