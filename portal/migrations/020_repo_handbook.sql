-- Repository handbook build lease (P2a).
-- Design: docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md §8.2.
ALTER TABLE repositories ADD COLUMN handbook_lease_until DATETIME(3) NULL AFTER handbook_stats;
