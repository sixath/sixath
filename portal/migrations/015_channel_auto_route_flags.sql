-- portal/migrations/015_channel_auto_route_flags.sql
ALTER TABLE channels
  ADD COLUMN auto_route_enabled TINYINT(1) NOT NULL DEFAULT 1 COMMENT 'master auto-route switch' AFTER allowed_agents,
  ADD COLUMN auto_route_mention TINYINT(1) NOT NULL DEFAULT 1 COMMENT '@Agent mention routing' AFTER auto_route_enabled,
  ADD COLUMN auto_route_classifier TINYINT(1) NOT NULL DEFAULT 1 COMMENT 'classifier when no @' AFTER auto_route_mention;
