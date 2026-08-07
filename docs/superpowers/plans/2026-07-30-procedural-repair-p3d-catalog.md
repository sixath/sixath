# P3-D Procedural Disable / Strengthen / Observe Plan

> Branch `feat/p3a-failure-signal`. No commit unless asked.

**Goal:** In-process procedural catalog: candidate→active after min_support FailureSignals; Disable; hit counters for Prefetch/Skill router.

**Architecture:** `ProceduralCatalog` owns entries; hand-written bindings seed it; FailureSignal strengthens by code; Match/Prefetch/Router only see `active` && !disabled.

**Spec:** umbrella §7.

## Out of scope

MemoryStore kind=procedural persistence (P3-E), full regression suite, GC.
