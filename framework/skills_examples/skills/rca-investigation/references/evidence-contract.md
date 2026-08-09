# RCA tool evidence contract

All RCA tools (`jaeger_trace`, `es_log_query`, `rca_grep`, `rca_glob`, `rca_read`) return a map shaped like:

| Field | Meaning |
|-------|---------|
| `ok` | `true` on success; `false` on failure |
| `error` | Human-readable message when `ok=false` |
| `error_code` | `transient` (retryable) or `permanent` (do not retry blindly) |
| `evidence_refs` | Array of `{kind, id, ...}` used by EvidenceGate |

## Kind conventions

| kind | Typical id |
|------|------------|
| `jaeger_trace` | trace id |
| `es_log_query` | query key / hit summary id |
| `repo` / path-line | `path:line` or repo-relative locus from code tools |

Final answers that assert a root cause should be supported by at least one successful ref from **jaeger_trace** or **es_log_query** (EvidenceGate default), plus code locus when claiming a bug site.
