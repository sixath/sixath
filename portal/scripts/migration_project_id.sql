-- migration_project_id.sql
-- Add project_id column to resources table for project-based resource grouping.
-- DEFAULT '' means the resource is not in any project (backward-compatible).

ALTER TABLE resources ADD COLUMN project_id VARCHAR(36) DEFAULT '' NOT NULL AFTER home_org_id;
CREATE INDEX idx_resources_project_id ON resources(project_id);
