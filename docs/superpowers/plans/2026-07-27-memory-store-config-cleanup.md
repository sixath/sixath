# MemoryStore P2-G Config Cleanup Plan

**Goal:** `memory_store:` nested YAML + hard-rename agent write env; document FTS keep.

**Spec:** `docs/superpowers/specs/2026-07-27-memory-store-config-cleanup-design.md`

## Tasks

1. framework config: `MemoryStore` block + Normalize + Load empty-check + tests
2. portal: SetPortalAgentExtra apply write_enabled; env rename + tests
3. docs: agent_extra.yaml, memory-integration, facade §8.7
