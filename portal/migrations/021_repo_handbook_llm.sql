-- Repository handbook LLM layer (P2b): per-repo model override, LLM state and the LLM run lease.
-- Design: docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md §7.2, §8.3.
ALTER TABLE repositories
  ADD COLUMN handbook_model VARCHAR(255) NOT NULL DEFAULT '' AFTER handbook_lease_token,
  ADD COLUMN handbook_llm JSON NULL AFTER handbook_model,
  ADD COLUMN handbook_llm_lease_until DATETIME(3) NULL AFTER handbook_llm,
  ADD COLUMN handbook_llm_lease_token VARCHAR(36) NULL AFTER handbook_llm_lease_until;
