-- P1b: agent ↔ hub asset bindings (Loadout explicit binds).
CREATE TABLE IF NOT EXISTS agent_asset_bindings (
    agent_id   VARCHAR(36)  NOT NULL,
    asset_kind VARCHAR(64)  NOT NULL,
    asset_id   VARCHAR(256) NOT NULL,
    hub        VARCHAR(64)  NOT NULL DEFAULT 'local',
    name       VARCHAR(256) NOT NULL DEFAULT '',
    status     VARCHAR(32)  NOT NULL DEFAULT 'active',
    priority   INT          NOT NULL DEFAULT 0,
    owner_id   VARCHAR(36)  NULL,
    visibility VARCHAR(32)  NULL,
    metadata   JSON         NULL,
    created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (agent_id, asset_kind, asset_id),
    INDEX idx_aab_agent (agent_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
