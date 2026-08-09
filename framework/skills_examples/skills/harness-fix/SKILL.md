---
name: harness-fix
description: >
  Use when tool failures repeat, ERRORS.md has pending entries, or the user asks to
  harden the harness so the same mistake cannot happen again. Reads .learnings/ERRORS.md,
  proposes a Skill create/patch via skill_manage, and/or a declarative block rule in
  harness/hooks.yaml via write_file (danger confirm). Wait for user confirm before
  claiming the fix is applied.
tags: [harness, growth, learnings, skill_manage, g4]
scope: [chat, portal]
allowed_tools:
  - load_skill
  - read_skill_file
  - skills_list
  - skill_view
  - append_learning
  - skill_manage
  - write_file
  - read_file
---

# Harness Fix (G4 / G4.1)

Turn **failure patterns** into **Skill SOP** and/or **declarative Before-block hooks** so the agent does not repeat the same mistake. Do **not** invent disk writes that the user has not confirmed.

## Choose output

| Failure kind | Prefer |
|--------------|--------|
| Process / SOP mistake | `skill_manage` create/patch |
| Deterministic dangerous call (regex-able args) | `harness/hooks.yaml` rule (`action: block`) |

## Workflow — Skill

1. **Collect** — Read `.learnings/ERRORS.md` + transcript errors.
2. **Propose** — `skill_manage` create/patch → expect `status=pending`.
3. **Wait** — User confirm → only then claim on-disk.

## Workflow — hooks.yaml (G4.1)

1. Read existing `harness/hooks.yaml` if present (`read_file`).
2. Propose via `write_file` path=`harness/hooks.yaml` (danger path → confirm card).
3. Schema (version 1), example:

```yaml
version: 1
rules:
  - id: block-pipe-sh
    tools: [terminal]
    match:
      param: command
      regex: "(?i)curl.*\\|.*sh"
    action: block
    reason: "piped curl|sh blocked by harness hook"
```

- `tools` empty = all tools; omit `match` = always block listed tools (use sparingly).
- Only `action: block` is supported.
- Loaded each chat turn from the agent workspace; bad YAML is skipped (logged).

## Rules

- Never auto-apply without UI confirm when the tool returns `pending`.
- Do not bypass confirm with shell edits of `skills/` or `harness/hooks.yaml`.
- Skip one-off transient noise.

## References

- `.learnings/ERRORS.md` — `SATH_GROWTH_FAILURE_CAPTURE=1`
- `portal/docs/growth-g4-failure-fix.md`
