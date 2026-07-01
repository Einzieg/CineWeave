ALTER TABLE canonical_assets
  ADD COLUMN IF NOT EXISTS manual_override BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS stale_state TEXT NOT NULL DEFAULT 'fresh',
  ADD COLUMN IF NOT EXISTS edited_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ NULL;

ALTER TABLE storyboard_shots
  ADD COLUMN IF NOT EXISTS manual_override BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS stale_state TEXT NOT NULL DEFAULT 'fresh',
  ADD COLUMN IF NOT EXISTS edited_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ NULL;

ALTER TABLE shot_asset_requirements
  ADD COLUMN IF NOT EXISTS manual_override BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS stale_state TEXT NOT NULL DEFAULT 'fresh',
  ADD COLUMN IF NOT EXISTS edited_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ NULL;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'canonical_assets_stale_state_check'
  ) THEN
    ALTER TABLE canonical_assets
      ADD CONSTRAINT canonical_assets_stale_state_check
      CHECK (stale_state IN ('fresh', 'upstream_changed', 'needs_regeneration'));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'storyboard_shots_stale_state_check'
  ) THEN
    ALTER TABLE storyboard_shots
      ADD CONSTRAINT storyboard_shots_stale_state_check
      CHECK (stale_state IN ('fresh', 'upstream_changed', 'needs_regeneration'));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'shot_asset_requirements_stale_state_check'
  ) THEN
    ALTER TABLE shot_asset_requirements
      ADD CONSTRAINT shot_asset_requirements_stale_state_check
      CHECK (stale_state IN ('fresh', 'upstream_changed', 'needs_regeneration'));
  END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS idx_canonical_assets_project_manual_override
  ON canonical_assets(project_id, manual_override);

CREATE INDEX IF NOT EXISTS idx_canonical_assets_project_stale_state
  ON canonical_assets(project_id, stale_state);

CREATE INDEX IF NOT EXISTS idx_storyboard_shots_project_manual_override
  ON storyboard_shots(project_id, manual_override);

CREATE INDEX IF NOT EXISTS idx_storyboard_shots_project_stale_state
  ON storyboard_shots(project_id, stale_state);

CREATE INDEX IF NOT EXISTS idx_shot_asset_requirements_project_manual_override
  ON shot_asset_requirements(project_id, manual_override);

CREATE INDEX IF NOT EXISTS idx_shot_asset_requirements_project_stale_state
  ON shot_asset_requirements(project_id, stale_state);

INSERT INTO schema_migrations(version) VALUES ('000014_editable_creative_objects')
ON CONFLICT (version) DO NOTHING;
