DELETE FROM prompt_versions
WHERE metadata->>'seed' = 'project_review_center';

DELETE FROM prompt_templates
WHERE organization_id IS NULL
  AND template_key = 'project_review_agent'
  AND is_system = true;

DROP INDEX IF EXISTS idx_review_items_entity;
DROP INDEX IF EXISTS idx_review_items_category;
DROP INDEX IF EXISTS idx_review_items_status;
DROP INDEX IF EXISTS idx_review_items_project;
DROP INDEX IF EXISTS idx_review_runs_status;
DROP INDEX IF EXISTS idx_review_runs_project;
DROP TRIGGER IF EXISTS review_items_set_updated_at ON review_items;
DROP TABLE IF EXISTS review_items;
DROP TABLE IF EXISTS review_runs;

DELETE FROM schema_migrations WHERE version = '000022_project_review_center';
