-- Email/password auth + org invites (Phase 2 Task 1)

ALTER TABLE users
  ADD COLUMN email VARCHAR(255) NULL DEFAULT NULL AFTER name,
  ADD COLUMN password_hash VARCHAR(255) NULL DEFAULT NULL AFTER email,
  ADD COLUMN email_verified_at DATETIME(3) NULL DEFAULT NULL AFTER password_hash,
  ADD UNIQUE INDEX idx_users_email (email);

CREATE TABLE IF NOT EXISTS org_invites (
    id          VARCHAR(36)  NOT NULL PRIMARY KEY,
    org_id      VARCHAR(36)  NOT NULL,
    token_hash  CHAR(64)     NOT NULL,
    created_by  VARCHAR(36)  NOT NULL,
    max_uses    INT          NOT NULL DEFAULT 1,
    used_count  INT          NOT NULL DEFAULT 0,
    expires_at  DATETIME(3)  NULL DEFAULT NULL,
    revoked_at  DATETIME(3)  NULL DEFAULT NULL,
    created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    INDEX idx_org_invites_org_id (org_id),
    INDEX idx_org_invites_token_hash (token_hash),
    INDEX idx_org_invites_created_by (created_by)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS email_verify_tokens (
    token_hash CHAR(64)    NOT NULL PRIMARY KEY,
    user_id    VARCHAR(36) NOT NULL,
    expires_at DATETIME(3) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    INDEX idx_email_verify_tokens_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
