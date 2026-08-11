# ES Exclude Data Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep elasticsearch in `framework/datasource` for RCA connections, but never expose it through `list_tables` / `describe_table` / `execute_read`; ES queries stay on `es_log_query` + `http_request`.

**Architecture:** Filter ES configs out of Portal `registerDatasourceTools` before MultiExecutor registration; prompt lists only data-capable bindings and adds an ES routing hint; optional execute-time reject if ES type is resolved.

**Tech Stack:** Go (Portal chat package, framework tool/data), zijian skills markdown.

**Spec:** `docs/superpowers/specs/2026-08-10-es-exclude-data-tools-design.md`

---

## File map

| File | Responsibility |
|------|----------------|
| `portal/internal/chat/datasource_prompt.go` | `SkipDataTools` / ES filter + routing hint |
| `portal/internal/chat/agent_builder.go` | Skip ES in data registration; ES-only ok |
| `portal/internal/chat/datasource_prompt_test.go` + `agent_builder_test.go` | Coverage |
| `framework/tool/data/*.go` | Optional ES type reject |
| `E:/sixath/workspace/zijian/skills/scheduling-flow-trace/SKILL.md` | Align wording; no hard-coded URL as sole truth |

---

### Task 1: Prompt + binding flag

- [ ] Add `SkipDataTools bool` to `DatasourceBinding`
- [ ] `FormatDatasourcePrompt`: only list `!SkipDataTools` as data sources; if any skipped ES (or `hasESHint`), append fixed routing sentence
- [ ] Tests
- [ ] Commit

### Task 2: `registerDatasourceTools` filter

- [ ] `isElasticsearchType`
- [ ] Skip register for ES; mark `SkipDataTools=true`; do not count as hard failure
- [ ] ES-only agent: success, no data trio tools
- [ ] MySQL+ES: trio only for MySQL
- [ ] Tests via `BuildRegistry`
- [ ] Commit

### Task 3: Defense + skills

- [ ] `execute_read` (and list/describe if cheap): if registry type is ES → clear error
- [ ] Skill wording pass
- [ ] Commit
