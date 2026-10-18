-- portal/migrations/023_weixin_ilink_channel.sql
-- weixin_ilink 渠道（docs/superpowers/specs/2026-10-18-weixin-ilink-channel-design.md §5）。
-- 启动时 GORM AutoMigrate 自动加列 / 建表；本文件仅作手工修复与记录。
ALTER TABLE channels
  ADD COLUMN ilink_bot_token VARCHAR(512) NULL,
  ADD COLUMN ilink_bot_id VARCHAR(128) NULL,
  ADD COLUMN ilink_owner_user_id VARCHAR(128) NULL,
  ADD COLUMN ilink_base_url VARCHAR(512) NULL,
  ADD COLUMN ilink_generation INT NOT NULL DEFAULT 0,
  ADD COLUMN ilink_session_expired_at DATETIME(3) NULL;

CREATE TABLE IF NOT EXISTS channel_ilink_peers (
  channel_uuid VARCHAR(36) NOT NULL,
  peer_id VARCHAR(128) NOT NULL,
  allowed TINYINT(1) NOT NULL DEFAULT 0,
  remark VARCHAR(128) NULL,
  context_token VARCHAR(512) NULL,
  context_token_at DATETIME(3) NULL,
  last_inbound_at DATETIME(3) NULL,
  last_preview VARCHAR(160) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (channel_uuid, peer_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS channel_ilink_state (
  channel_uuid VARCHAR(36) NOT NULL PRIMARY KEY,
  generation INT NOT NULL DEFAULT 0,
  get_updates_buf TEXT NULL,
  updated_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
