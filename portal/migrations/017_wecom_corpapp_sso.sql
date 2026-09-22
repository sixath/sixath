-- WeCom CorpApp SSO: external identity mapping + ephemeral OAuth state/tickets

CREATE TABLE IF NOT EXISTS user_identities (
  provider VARCHAR(32) NOT NULL,
  subject VARCHAR(128) NOT NULL,
  user_id VARCHAR(36) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (provider, subject),
  INDEX idx_user_identities_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS auth_ephemeral (
  id VARCHAR(64) NOT NULL,
  kind VARCHAR(32) NOT NULL,
  payload_json TEXT NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  consumed_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  INDEX idx_auth_ephemeral_kind (kind),
  INDEX idx_auth_ephemeral_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
