DROP INDEX IF EXISTS idx_shot_asset_requirements_project_stale_state;
DROP INDEX IF EXISTS idx_shot_asset_requirements_project_manual_override;
DROP INDEX IF EXISTS idx_storyboard_shots_project_stale_state;
DROP INDEX IF EXISTS idx_storyboard_shots_project_manual_override;
DROP INDEX IF EXISTS idx_canonical_assets_project_stale_state;
DROP INDEX IF EXISTS idx_canonical_assets_project_manual_override;

ALTER TABLE shot_asset_requirements
  DROP CONSTRAINT IF EXISTS shot_asset_requirements_stale_state_check,
  DROP COLUMN IF EXISTS edited_at,
  DROP COLUMN IF EXISTS edited_by,
  DROP COLUMN IF EXISTS stale_state,
  DROP COLUMN IF EXISTS manual_override;

ALTER TABLE storyboard_shots
  DROP CONSTRAINT IF EXISTS storyboard_shots_stale_state_check,
  DROP COLUMN IF EXISTS edited_at,
  DROP COLUMN IF EXISTS edited_by,
  DROP COLUMN IF EXISTS stale_state,
  DROP COLUMN IF EXISTS manual_override;

ALTER TABLE canonical_assets
  DROP CONSTRAINT IF EXISTS canonical_assets_stale_state_check,
  DROP COLUMN IF EXISTS edited_at,
  DROP COLUMN IF EXISTS edited_by,
  DROP COLUMN IF EXISTS stale_state,
  DROP COLUMN IF EXISTS manual_override;
