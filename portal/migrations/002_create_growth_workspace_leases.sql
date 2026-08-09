-- 002_create_growth_workspace_leases.sql
-- Workspace-level review lease for multi-portal mutual exclusion (spec §6).
CREATE TABLE growth_workspace_leases (
    workspace_key VARCHAR(384)  NOT NULL PRIMARY KEY,
    holder_id     VARCHAR(128)  NOT NULL,
    expires_at    DATETIME(3)   NOT NULL,
    updated_at    DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    INDEX idx_growth_workspace_leases_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
