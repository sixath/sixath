-- 004_growth_curator_states.sql
-- R2b: per-workspace Curator last-run cursor (spec growth-r1-r3-feasibility §R2b).
CREATE TABLE growth_curator_states (
    workspace_key  VARCHAR(384)  NOT NULL PRIMARY KEY,
    last_curator_at DATETIME(3),
    last_error      TEXT,
    updated_at      DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
