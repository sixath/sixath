-- P2-A: denormalized user_id for scope=user units (scope_id already holds user_id).
ALTER TABLE memory_units
  ADD COLUMN user_id VARCHAR(36) NULL AFTER agent_id,
  ADD INDEX idx_mu_user (user_id, status);
