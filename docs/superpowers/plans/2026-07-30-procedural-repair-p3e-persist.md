# P3-E Procedural Persist Pilot Plan

> Branch `feat/p3a-failure-signal`. No commit unless asked.

**Goal:** Persist `kind=procedural` units (gated commit), exclude them from fact Recall, pilot `zone-4100-agent` + `auto_commit` after catalog activate.

**Decisions:**
- DB column `kind` VARCHAR + default `fact`; metadata mirrors `kind` / `procedural_status`
- `Facade.CommitProceduralRepair` only write path (Remember still blocks bare procedural)
- Skip D2 + vector for procedural
- Recall/List default exclude procedural (`Kind=""` → fact-only)
- Pilot match: agent id **or** name in `pilot_agents` (`ContextKeyAgentName`)

**Out of scope:** full Evolution-SOP LLM extract, prefer-mode enforcement, Neo4j.

## Files

| File | Change |
|------|--------|
| `framework/memory/kind.go` | KindMatchesFilter, IsPilotAgent |
| `framework/memory/procedural_commit.go` | CommitProceduralRepair five gates |
| `framework/memory/procedural_catalog.go` | OnActivated hook |
| `framework/memory/store_prefetch_backend.go` | load persisted procedural |
| `portal/migrations/011_memory_units_kind.sql` | kind column |
| `portal/internal/data/memory_units_*.go` | kind write/filter |
| `portal/internal/chat/procedural_binding.go` | auto_commit + merge persisted |
| docs + umbrella §13 | P3-E delivered |

## Acceptance

1. Default config: no auto_commit writes
2. Commit requires pilot + FailureSignal + valid binding
3. Fact Recall excludes procedural; Kind=procedural lane returns them
4. Remember(kind=procedural) still blocked
