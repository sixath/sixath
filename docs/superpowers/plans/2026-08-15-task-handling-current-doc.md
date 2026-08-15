# Task Handling Current-Doc Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.  
> Spec confirmed 2026-08-15 on branch `feature/mea-minimal-subset`.  
> **Do not commit unless the user asks.** Prefer one logical commit per Task when committing is requested.

**Goal:** Make the as-is chat task-handling doc discoverable and keep it accurate against the current codebase (docs + cross-links only; no runtime behavior changes).

**Architecture:** The canonical narrative already lives at `docs/superpowers/specs/2026-08-15-task-handling-current-design.md`. This plan adds inbound links from MEA / portal docs, then runs a short verification checklist (grep/read against key symbols) so drift is caught before someone trusts the doc for onboarding or troubleshooting.

**Tech Stack:** Markdown docs; existing Go sources under `portal/` and `framework/` for verification only (no new packages).

**Spec:** [docs/superpowers/specs/2026-08-15-task-handling-current-design.md](../specs/2026-08-15-task-handling-current-design.md)

---

## File map

| File | Responsibility |
|------|----------------|
| Exists `docs/superpowers/specs/2026-08-15-task-handling-current-design.md` | Canonical end-to-end + troubleshooting narrative (already written) |
| Modify `docs/superpowers/specs/2026-08-12-mea-minimal-subset-design.md` | Link to task-handling current doc from header / related list |
| Modify `docs/superpowers/specs/2026-08-12-mea-m05-chat-wire-design.md` | Link to task-handling current doc (entry conditions live here) |
| Modify `docs/superpowers/specs/2026-08-13-mea-m1-llm-auditor-design.md` | Link for LLM acceptance path context |
| Modify `portal/docs/mea-m0.md` | Operator-facing pointer to full path (Portal → ReAct → SSE) |
| Modify task-handling spec **only where verification finds factual drift** | Keep docs honest; still no Go/Web runtime changes unless user expands scope |

**Out of scope (do not implement in this plan):**

- Changing `useMEA` conditions, SSE shapes, or MEA/ReAct wiring
- UI / Agent form copy changes
- WeCom-specific deep dive beyond “same Chat stream path”
- New automated Go tests solely for documentation

---

### Task 1: Cross-link MEA design specs → task-handling doc

**Files:**
- Modify: `docs/superpowers/specs/2026-08-12-mea-minimal-subset-design.md` (header / related links near top)
- Modify: `docs/superpowers/specs/2026-08-12-mea-m05-chat-wire-design.md` (after title or 「进入条件」)
- Modify: `docs/superpowers/specs/2026-08-13-mea-m1-llm-auditor-design.md` (header related links)

- [ ] **Step 1: Read each file’s existing related-link block**

Open the three MEA specs and note where sibling links already appear (e.g. M0 ↔ M0.5).

- [ ] **Step 2: Add one inbound link in each**

Use the same relative style as existing links:

```markdown
> 端到端现状（入口分流 / ReAct / SSE / 排障）：[task-handling current](./2026-08-15-task-handling-current-design.md)
```

Place next to other related-spec lines; do not rewrite MEA requirements.

- [ ] **Step 3: Spot-check links resolve**

From repo root, confirm the target exists:

```bash
Test-Path docs/superpowers/specs/2026-08-15-task-handling-current-design.md
```

Expected: `True`

- [ ] **Step 4: Commit (only if user asked)**

```bash
git add docs/superpowers/specs/2026-08-12-mea-minimal-subset-design.md \
  docs/superpowers/specs/2026-08-12-mea-m05-chat-wire-design.md \
  docs/superpowers/specs/2026-08-13-mea-m1-llm-auditor-design.md
git commit -m "docs: link MEA specs to task-handling current narrative"
```

---

### Task 2: Cross-link portal operator doc

**Files:**
- Modify: `portal/docs/mea-m0.md`

- [ ] **Step 1: Read `portal/docs/mea-m0.md` intro**

Confirm the short M0.5 entry-condition paragraph is still accurate vs spec §2/§3. If wording says merely 「消息含」fences, tighten to 「成功解析出非空」. If it claims fences are always stripped from persistence, soften to match code: strip only when `ParseMEA*` returns `ok=true` (invalid/empty fences may remain).

- [ ] **Step 2: Add pointer after the M0.5 blurb**

```markdown
**端到端走读（含纯 ReAct、SSE、排障）：** [docs/superpowers/specs/2026-08-15-task-handling-current-design.md](../../docs/superpowers/specs/2026-08-15-task-handling-current-design.md)
```

Path is relative from `portal/docs/` → repo `docs/` (`../..` then `docs/...`).

- [ ] **Step 3: Verify relative path**

From `portal/docs/`, the linked file must exist at `../../docs/superpowers/specs/2026-08-15-task-handling-current-design.md`.

```bash
Test-Path docs/superpowers/specs/2026-08-15-task-handling-current-design.md
```

Expected: `True` (repo-root relative). Mentally confirm `portal/docs/../../docs/...` = `docs/...`.

- [ ] **Step 4: Commit (only if user asked)**

```bash
git add portal/docs/mea-m0.md
git commit -m "docs(portal): point mea-m0 to end-to-end task-handling narrative"
```

---

### Task 3: Verify key claims against code

**Files:**
- Read-only: `portal/internal/service/chat.go` (`useMEA`)
- Read-only: `portal/internal/chat/mea_parse.go`, `mea_flag.go`, `mea_run.go`
- Read-only: `portal/internal/service/mea_stream.go`
- Read-only: `portal/internal/chatsse/sse.go`
- Read-only: `framework/mea/orchestrator.go`, `framework/agent/react_agent.go`
- Possibly modify: `docs/superpowers/specs/2026-08-15-task-handling-current-design.md` **only if** a factual mismatch is found

- [ ] **Step 1: Verify `useMEA` three conditions**

```bash
rg -n "useMEA :=" portal/internal/service/chat.go
```

Expected: conditions include `(len(meaChecks) > 0 || len(meaAcceptance) > 0)`, `MEAEnabledForAgent`, and non-empty `Workspace`.

- [ ] **Step 2: Verify parse failure does not set checks**

```bash
rg -n "return clean, nil, false" portal/internal/chat/mea_parse.go
```

Expected: invalid JSON / empty / no valid type returns `ok=false`. Confirm callers only assign on `ok`:

```bash
rg -n "ParseMEAChecks|ParseMEAAcceptance" portal/internal/service/chat.go
```

Expected: `if clean, ..., ok := ...; ok {` pattern (fence stripped only when ok).

- [ ] **Step 3: Verify SSE `mea` mapping**

```bash
rg -n "ChatStreamEventMEA|WriteEvent\\(w, \"mea\"" portal/internal/chatsse/sse.go portal/internal/service/mea_stream.go
```

Expected: `phase` emissions `started` / `round` / `finished` in `mea_stream.go`; `event: mea` in `sse.go`.

- [ ] **Step 4: Verify MEA wraps ReAct executor**

```bash
rg -n "streamAgentEvents|RunRulesMEA|ExecutorFunc" portal/internal/service/mea_stream.go
```

Expected: Executor callback calls `streamAgentEvents` / `RunEvents` path; outer loop via `RunRulesMEA`.

- [ ] **Step 5: If any claim in the spec is wrong, patch the spec**

Edit only the mismatched bullets/tables in `2026-08-15-task-handling-current-design.md`. Do **not** “fix” behavior in Go under this plan.

- [ ] **Step 6: Commit (only if user asked and files changed)**

```bash
git add docs/superpowers/specs/2026-08-15-task-handling-current-design.md
git commit -m "docs: correct task-handling narrative after code verification"
```

If nothing changed, skip commit.

---

### Task 4: Smoke-read handoff checklist

**Files:** none (manual)

- [x] **Step 1: Read spec §2 and §6.1 aloud mentally as a new engineer**

Confirm they can answer: “Why didn’t I get `event: mea`?” using only the doc.

- [x] **Step 2: Confirm outbound links from the task-handling spec still resolve**

Open related links listed in the spec header (M0 / M0.5 / M1 / `portal/docs/mea-m0.md`).

- [x] **Step 3: Mark plan complete**

No further code. Hand off: doc is discoverable + verified.

---

## Done when

- [x] MEA M0 / M0.5 / M1 specs and `portal/docs/mea-m0.md` link to the task-handling current doc
- [x] Verification checklist in Task 3 passes (or spec corrected if drift found)
- [x] No runtime Go/Web changes introduced by this plan
