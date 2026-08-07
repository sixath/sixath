# P3-C Procedural Binding Slots Implementation Plan

> Nested repos. **Do not commit unless asked.** Branch: `feat/p3a-failure-signal`.

**Goal:** Hand-written `ProceduralBinding` registry with validate-unknown-tools; expose via Prefetch + Skill router (suggest mode).

**Architecture:** Config `memory_store.procedural_repair.bindings` → validated registry → PrefetchPart `procedural` + skill_router suggest block. No MemoryStore procedural writes (still blocked by P3-B).

**Spec:** umbrella §6.

## Files

| File | Change |
|------|--------|
| `framework/memory/procedural_binding.go` | types, Validate, Match, Format, ResolveTaskFamily |
| `framework/memory/procedural_binding_test.go` | tests |
| `framework/memory/store_prefetch_backend.go` | append binding hints |
| `framework/config/tool_guardrails.go` | MemoryProceduralRepair config + Normalize |
| `portal/internal/chat/procedural_binding.go` | store registry, Set from agent_extra |
| `portal/internal/chat/skill_router.go` | inject skill suggest from bindings |
| `portal/internal/chat/memory_prefetch_bootstrap.go` | wire Bindings |
| `portal/internal/chat/portal_agent_extra.go` | load |
| docs + umbrella §13 | |

## Out of scope

auto_commit, kind column, disable/strengthen (P3-D), prefer mode enforcement beyond config flag.
