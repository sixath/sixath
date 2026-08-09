-- User / Org / ACL tables + chat_sessions.user_id (Task 3)

CREATE TABLE IF NOT EXISTS users (
    id         VARCHAR(36)  NOT NULL PRIMARY KEY,
    name       VARCHAR(128) NOT NULL,
    created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS orgs (
    id         VARCHAR(36)  NOT NULL PRIMARY KEY,
    name       VARCHAR(128) NOT NULL,
    created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS org_members (
    org_id     VARCHAR(36) NOT NULL,
    user_id    VARCHAR(36) NOT NULL,
    role       VARCHAR(16) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (org_id, user_id),
    INDEX idx_org_members_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS user_tokens (
    token_hash CHAR(64)    NOT NULL PRIMARY KEY,
    user_id    VARCHAR(36) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    INDEX idx_user_tokens_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS resources (
    id             VARCHAR(36)  NOT NULL PRIMARY KEY,
    type           VARCHAR(16)  NOT NULL,
    name           VARCHAR(128) NOT NULL,
    owner_user_id  VARCHAR(36)  NOT NULL,
    visibility     VARCHAR(16)  NOT NULL,
    home_org_id    VARCHAR(36)  NULL DEFAULT NULL,
    bound_agent_id VARCHAR(36)  NULL DEFAULT NULL,
    payload_ref    VARCHAR(36)  NOT NULL,
    created_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE INDEX idx_resource_type_payload (type, payload_ref),
    INDEX idx_resources_owner_user_id (owner_user_id),
    INDEX idx_resources_home_org_id (home_org_id),
    INDEX idx_resources_bound_agent_id (bound_agent_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS resource_grants (
    resource_id  VARCHAR(36) NOT NULL,
    grantee_type VARCHAR(16) NOT NULL,
    grantee_id   VARCHAR(36) NOT NULL,
    perm         VARCHAR(16) NOT NULL,
    created_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (resource_id, grantee_type, grantee_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- chat_sessions.user_id: empty default until Task 5 backfill
ALTER TABLE chat_sessions
  ADD COLUMN user_id VARCHAR(36) NOT NULL DEFAULT '' AFTER agent_id,
  ADD INDEX idx_chat_sessions_user_id (user_id);
