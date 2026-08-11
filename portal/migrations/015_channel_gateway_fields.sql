-- portal/migrations/015_channel_gateway_fields.sql
ALTER TABLE channels
  ADD COLUMN bot_id VARCHAR(128) NULL COMMENT 'wecom_bot bot_id' AFTER webhook_url,
  ADD COLUMN bot_secret VARCHAR(256) NULL COMMENT 'wecom_bot secret' AFTER bot_id,
  ADD COLUMN bot_names JSON NULL COMMENT 'wecom_bot mention names' AFTER bot_secret,
  ADD COLUMN ws_url VARCHAR(512) NULL COMMENT 'wecom_bot websocket url' AFTER bot_names,
  ADD COLUMN corp_id VARCHAR(64) NULL AFTER ws_url,
  ADD COLUMN corp_secret VARCHAR(256) NULL AFTER corp_id,
  ADD COLUMN default_reply_mode VARCHAR(16) NULL COMMENT 'async|sync for webhook' AFTER corp_secret;

CREATE TABLE IF NOT EXISTS channel_runtime_status (
  channel_id VARCHAR(64) NOT NULL PRIMARY KEY,
  state VARCHAR(32) NOT NULL,
  last_heartbeat_at DATETIME(3) NOT NULL,
  last_error TEXT NULL,
  reconnect_attempt INT NOT NULL DEFAULT 0,
  reconnect_in_ms INT NOT NULL DEFAULT 0,
  gateway_instance_id VARCHAR(128) NULL,
  updated_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
