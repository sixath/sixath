CREATE TABLE memory_units (
    id                VARCHAR(36)  NOT NULL PRIMARY KEY,
    scope_type        ENUM('user','session','agent') NOT NULL,
    scope_id          VARCHAR(36)  NOT NULL,
    agent_id          VARCHAR(36)  NULL,
    content           TEXT         NOT NULL,
    content_hash      CHAR(64)     NOT NULL,
    status            ENUM('active','superseded','deleted') NOT NULL DEFAULT 'active',
    supersedes_id     VARCHAR(36)  NULL,
    source_session_id VARCHAR(36)  NULL,
    metadata          JSON         NULL,
    created_at        DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at        DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    INDEX idx_mu_scope (scope_type, scope_id, status),
    INDEX idx_mu_hash (content_hash),
    INDEX idx_mu_session (source_session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
