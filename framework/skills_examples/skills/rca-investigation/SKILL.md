---
name: rca-investigation
description: >
  Use when diagnosing production failures with RCA tools: start from a trace_id via
  jaeger_trace, enrich with es_log_query, then narrow to code with rca_grep / rca_glob /
  rca_read. Prefer this skill whenever the user provides a trace id, span error, or asks
  for root-cause analysis with Jaeger/ES/repo access.
tags: [rca, observability, jaeger, elasticsearch, debugging]
scope: [chat, portal]
allowed_tools:
  - load_skill
  - read_skill_file
  - skills_list
  - skill_view
  - jaeger_trace
  - es_log_query
  - rca_grep
  - rca_glob
  - rca_read
---

# RCA Investigation

Thin workflow skill for Sixath RCA tools. **Do not invent traces, logs, or file contents** — every claim must cite tool results (`evidence_refs` / `ok`).

## Standard order (lock)

0. **Quoted error (when present)** — `rca_grep` the user's original text, then `rca_read` the translation point. Do not use workspace `search_files` for application source.
1. **Trace** — `jaeger_trace` with `trace_id` (or service/operation search if id unknown).
2. **Logs** — `es_log_query(cluster="<elasticsearch tool name>", trace_id=...)` with the same `trace_id` (and time window / service filters if available). `cluster` is required (bound elasticsearch datasource name). A call without `cluster` does not succeed. The same task may call a second `cluster` for another bound ES. Prefer the English RPC / error code from step 0 over UI copy. `hit_status=empty` means the index and fields were valid but no documents matched. `index_error=unresolved` means the index pattern does not exist — pick a name from `suggested_index_patterns` and retry; do not treat it as missing logs.
3. **Code follow-up** — from error messages / stack frames / class names found above, use `rca_grep` → `rca_glob` → `rca_read` to pin files and lines.

Do **not** jump to speculative code failure lists. Grepping the user's quoted error is evidence gathering, not speculation. Never ask the user to restate the original question after context compression.

## Tool checklist

| Step | Tool | Success signal |
|------|------|----------------|
| 1 | `jaeger_trace` | `ok: true` and `evidence_refs` include kind `jaeger_trace` |
| 2 | `es_log_query` | `ok: true` and refs include kind `es_log_query` |
| 3 | `rca_grep` / `rca_glob` / `rca_read` | `ok: true` with `repo:path:line` style refs |

On `ok: false`:

- `error_code=transient` → retry once with backoff / narrower query; say so if still failing.
- `error_code=permanent` → stop that branch; report the error; do not fabricate data.
- `index_error=unresolved` → the query did not land on a real index; retry with `suggested_index_patterns`. This is not `insufficient evidence`.

## Closing the case

- Prefer a short causal chain: **symptom → span/log evidence → code locus → hypothesis**.
- If Jaeger/ES cannot be reached or return empty after honest attempts, conclude with explicit **证据不足** / `insufficient evidence` (EvidenceGate Soft allows this).
- Never claim a root cause that is not backed by at least one of: error span, log line, or code locus from tools.

## References

- `references/evidence-contract.md` — result fields agents must honor.
