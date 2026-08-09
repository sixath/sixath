## Summary

- Wire `GrowthWorker` with file-stub / real LLM proposers, optional combined review (`combined_review_enabled`), retry backoff + `DropPendingAfterMaxRetry`, and `growthwake` on pending.
- Add versioned SQL migrations (`001`–`003`): `chat_growth_states`, `growth_workspace_leases`, `review_retry_count`, `last_idle_check_at`.
- Add `/api/v1/growth/metrics`, idle sweep (`sweepIdle` + `ListIdleSessions`), memory state in review prompt (B1), and `growth.*` config (poll/idle intervals, LLM, max_retry).
- Task12 doc: `docs/growth-idle-polling.md`; example patch: `configs/growth_review_patch.example.json`.

## Test plan

- [x] `go test ./internal/biz/... ./internal/service/... -count=1 -short`
- [x] `go build ./cmd/backend/...`
- [ ] Apply migrations `001`–`003` on MySQL (or confirm AutoMigrate in dev)
- [ ] Enable in `config.yaml`:
  ```yaml
  growth:
    llm_review_enabled: true
    review_patch_file: "growth_review_patch.example.json"
  ```
- [ ] Optional: `bash scripts/integration_growth_lease.sh` (Docker MySQL, `//go:build integration`)

## Related

- Framework companion PR: [sixath/framework#1](https://github.com/sixath/framework/pull/1)
- Plan: `framework/docs/superpowers/plans/2026-05-10-growth-system-phase2.md` (framework repo)
