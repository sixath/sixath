-- R1: 会话父子链（Curator/Hermes 式折叠）；可为空。
ALTER TABLE chat_sessions
  ADD COLUMN parent_session_id VARCHAR(36) NULL DEFAULT NULL AFTER agent_id,
  ADD INDEX idx_chat_sessions_parent (parent_session_id);
