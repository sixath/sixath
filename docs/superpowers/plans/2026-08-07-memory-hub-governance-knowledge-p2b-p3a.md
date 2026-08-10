# Memory Hub — P2b Approve + P3a Knowledge Skeleton

> Nested: `framework/`, `portal/`, `web/`. **Do not commit unless asked.**

**Goal:** 补齐外部 skill **draft→active 人工确认**闭环；LocalKnowledge 增加可选 **wiki / codegraph** 搜索后端与 Capabilities 诚实声明（无完整引擎）。

---

## P2b — Approve

- [x] `POST /api/v1/agents/{id}/hub/assets/status` → `GovernanceWriter.SetStatus`
- [x] 单测：fake 未签名 Bind=draft；Approve=active 后进 Loadout
- [x] Agent Detail「确认激活」按钮

## P3a — Knowledge skeleton

- [x] `WikiSearcher` / `CodeGraphSearcher` 可选注入
- [x] Capabilities flags `wiki` / `code_graph` 仅当后端非 nil
- [x] `knowledge_search` 显式 `source=wiki|codegraph` 才调用；默认源仍不含二者
- [x] 接口骨架完成；**可搜实现见 [P3b](./2026-08-07-memory-hub-governance-knowledge-p3b.md)**

## Out of scope（P3a）

真实 Tencent HTTP；双向 sync；完整 Wiki 引擎（P3b 仅本地目录索引）。
