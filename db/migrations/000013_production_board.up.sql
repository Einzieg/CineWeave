ALTER TABLE canonical_assets
  ADD COLUMN IF NOT EXISTS review_status TEXT NOT NULL DEFAULT 'pending';

ALTER TABLE storyboard_shots
  ADD COLUMN IF NOT EXISTS review_status TEXT NOT NULL DEFAULT 'pending';

ALTER TABLE shot_asset_requirements
  ADD COLUMN IF NOT EXISTS review_status TEXT NOT NULL DEFAULT 'pending';

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'canonical_assets_review_status_check'
  ) THEN
    ALTER TABLE canonical_assets
      ADD CONSTRAINT canonical_assets_review_status_check
      CHECK (review_status IN ('pending', 'approved', 'rejected', 'needs_edit'));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'storyboard_shots_review_status_check'
  ) THEN
    ALTER TABLE storyboard_shots
      ADD CONSTRAINT storyboard_shots_review_status_check
      CHECK (review_status IN ('pending', 'approved', 'rejected', 'needs_edit'));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'shot_asset_requirements_review_status_check'
  ) THEN
    ALTER TABLE shot_asset_requirements
      ADD CONSTRAINT shot_asset_requirements_review_status_check
      CHECK (review_status IN ('pending', 'approved', 'rejected', 'needs_edit'));
  END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS canonical_assets_project_review_idx
  ON canonical_assets(project_id, review_status);

CREATE INDEX IF NOT EXISTS storyboard_shots_project_review_idx
  ON storyboard_shots(project_id, review_status);

CREATE INDEX IF NOT EXISTS shot_asset_requirements_project_review_idx
  ON shot_asset_requirements(project_id, review_status);

INSERT INTO schema_migrations(version) VALUES ('000013_production_board')
ON CONFLICT (version) DO NOTHING;
