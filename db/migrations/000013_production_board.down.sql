DROP INDEX IF EXISTS shot_asset_requirements_project_review_idx;
DROP INDEX IF EXISTS storyboard_shots_project_review_idx;
DROP INDEX IF EXISTS canonical_assets_project_review_idx;

ALTER TABLE shot_asset_requirements
  DROP CONSTRAINT IF EXISTS shot_asset_requirements_review_status_check,
  DROP COLUMN IF EXISTS review_status;

ALTER TABLE storyboard_shots
  DROP CONSTRAINT IF EXISTS storyboard_shots_review_status_check,
  DROP COLUMN IF EXISTS review_status;

ALTER TABLE canonical_assets
  DROP CONSTRAINT IF EXISTS canonical_assets_review_status_check,
  DROP COLUMN IF EXISTS review_status;

DELETE FROM schema_migrations WHERE version = '000013_production_board';
