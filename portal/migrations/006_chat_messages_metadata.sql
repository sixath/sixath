-- 消息元数据（执行时间线 timeline、后续 sources 等）
ALTER TABLE chat_messages
  ADD COLUMN metadata JSON NULL AFTER content;
