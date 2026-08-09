-- MCP Server registry + agent bindings (stdio/SSE transport configs).

CREATE TABLE IF NOT EXISTS mcp_servers (
    id           VARCHAR(36)  NOT NULL PRIMARY KEY,
    name         VARCHAR(128) NOT NULL,
    description  TEXT         NOT NULL,
    transport    VARCHAR(16)  NOT NULL,
    endpoint     VARCHAR(512) NOT NULL DEFAULT '',
    backend      VARCHAR(32)  NOT NULL DEFAULT '',
    command      VARCHAR(256) NOT NULL DEFAULT '',
    args_json    JSON         NULL,
    env_json     JSON         NULL,
    timeout_sec  INT          NOT NULL DEFAULT 60,
    created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    INDEX idx_mcp_servers_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS agent_mcp_servers (
    agent_id    VARCHAR(36) NOT NULL,
    server_id   VARCHAR(36) NOT NULL,
    sort_order  INT         NOT NULL DEFAULT 0,
    created_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (agent_id, server_id),
    CONSTRAINT fk_agent_mcp_servers_agent FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    CONSTRAINT fk_agent_mcp_servers_server FOREIGN KEY (server_id) REFERENCES mcp_servers(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
