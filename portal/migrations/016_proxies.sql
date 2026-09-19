-- Named HTTP/SOCKS5 egress proxies + optional Agent default binding.

CREATE TABLE IF NOT EXISTS proxies (
    id          VARCHAR(36) NOT NULL PRIMARY KEY,
    name        VARCHAR(128) NOT NULL,
    description TEXT NOT NULL,
    type        VARCHAR(16) NOT NULL,
    host        VARCHAR(256) NOT NULL,
    port        INT NOT NULL,
    user        VARCHAR(128) NOT NULL DEFAULT '',
    password    VARCHAR(256) NOT NULL DEFAULT '',
    no_proxy    JSON NULL,
    created_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    INDEX idx_proxies_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE agents ADD COLUMN proxy_id VARCHAR(36) NULL;
