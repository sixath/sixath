-- portal/migrations/014_channel_allowed_agents.sql
ALTER TABLE channels
  ADD COLUMN allowed_agents JSON NULL COMMENT 'agent UUID allowlist; empty/null => only default_agent' AFTER default_agent;
