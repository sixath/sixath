-- Repository registry: repos discovered under code roots, groups, and agent bindings.
-- Design: docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md §4.

CREATE TABLE IF NOT EXISTS repositories (
    id               VARCHAR(36)   NOT NULL PRIMARY KEY,
    code_root        VARCHAR(255)  NOT NULL,
    rel_path         VARCHAR(512)  NOT NULL,
    name             VARCHAR(256)  NOT NULL DEFAULT '',
    description      TEXT          NULL,
    tags             JSON          NULL,
    git_remote       VARCHAR(512)  NOT NULL DEFAULT '',
    git_branch       VARCHAR(256)  NOT NULL DEFAULT '',
    head_commit      VARCHAR(64)   NOT NULL DEFAULT '',
    sync_mode        VARCHAR(16)   NOT NULL DEFAULT 'registry_only',
    status           VARCHAR(16)   NOT NULL DEFAULT 'active',
    handbook_status  VARCHAR(16)   NOT NULL DEFAULT 'none',
    handbook_commit  VARCHAR(64)   NOT NULL DEFAULT '',
    handbook_version INT           NOT NULL DEFAULT 0,
    handbook_stats   JSON          NULL,
    owner_id         VARCHAR(36)   NOT NULL DEFAULT '',
    last_scanned_at  DATETIME(3)   NULL,
    created_at       DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at       DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_repo_root_rel (code_root, rel_path),
    INDEX idx_repo_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS repo_groups (
    id               VARCHAR(36)   NOT NULL PRIMARY KEY,
    name             VARCHAR(256)  NOT NULL,
    kind             VARCHAR(16)   NOT NULL,
    rule             JSON          NULL,
    auto_apply_new   TINYINT(1)    NOT NULL DEFAULT 1,
    handbook_status  VARCHAR(16)   NOT NULL DEFAULT 'none',
    handbook_version INT           NOT NULL DEFAULT 0,
    owner_id         VARCHAR(36)   NOT NULL DEFAULT '',
    created_at       DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at       DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    INDEX idx_rg_kind (kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS repo_group_members (
    group_id    VARCHAR(36)  NOT NULL,
    repo_id     VARCHAR(36)  NOT NULL,
    source      VARCHAR(16)  NOT NULL,
    state       VARCHAR(16)  NOT NULL DEFAULT 'active',
    created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (group_id, repo_id),
    INDEX idx_rgm_repo (repo_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS agent_repo_bindings (
    agent_id     VARCHAR(36)   NOT NULL,
    target_kind  VARCHAR(16)   NOT NULL,
    target_id    VARCHAR(256)  NOT NULL,
    mode         VARCHAR(16)   NOT NULL DEFAULT 'include',
    sub_paths    JSON          NULL,
    rule         JSON          NULL,
    priority     INT           NOT NULL DEFAULT 0,
    created_by   VARCHAR(128)  NOT NULL DEFAULT '',
    created_at   DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at   DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (agent_id, target_kind, target_id),
    INDEX idx_arb_target (target_kind, target_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS agent_effective_repos (
    agent_id     VARCHAR(36)  NOT NULL,
    repo_id      VARCHAR(36)  NOT NULL,
    via          JSON         NULL,
    sub_paths    JSON         NULL,
    computed_at  DATETIME(3)  NOT NULL,
    PRIMARY KEY (agent_id, repo_id),
    INDEX idx_aer_repo (repo_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
