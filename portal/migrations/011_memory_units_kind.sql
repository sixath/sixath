-- P3-E: memory_units.kind distinguishes fact vs procedural.
ALTER TABLE memory_units
  ADD COLUMN kind VARCHAR(32) NOT NULL DEFAULT 'fact' AFTER content,
  ADD INDEX idx_mu_kind (scope_type, kind, status);
