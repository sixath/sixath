## Summary

- Add `SkillReviewRunner`: transcript + skills index snapshot â†?`ValidatePatchBatch` + `ApplyPatchBatch` â†?`DefaultSkillsIndexTracker.Bump` (phase2 A1â€“A4).
- Add optional `runner_llm` with `NewLLMSkillProposer` / `NewLLMCombinedProposer`, JSON patch parsing (`patch_json.go`), and process metrics (L1/L2, E2).
- Add growth state machine spec (`docs/superpowers/specs/2026-05-18-growth-state-machine.md`); update acceptance checklist and phase2 plan.
- T1 regression: `TestSkillReviewRunner_T1FakeLLMPatchToFSAndIndexGen`.

## Test plan

- [x] `go test ./growth/... -count=1`
- [ ] Pair with portal PR: enable `growth.llm_review_enabled` + `review_patch_file` and verify patch writeback end-to-end

## Related

- Portal companion PR: [sixath/portal#1](https://github.com/sixath/portal/pull/1)
- Plan: `docs/superpowers/plans/2026-05-10-growth-system-phase2.md`
