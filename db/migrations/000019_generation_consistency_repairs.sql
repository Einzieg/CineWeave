-- +goose Up

SET search_path TO public;

ALTER TABLE project_video_production_rebuilds
    ADD COLUMN reason TEXT NOT NULL DEFAULT 'profile_change',
    ADD COLUMN target_configuration JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN target_configuration_hash TEXT;

ALTER TABLE project_video_production_rebuilds
    ADD CONSTRAINT project_video_production_rebuilds_reason_check
        CHECK (reason IN ('profile_change', 'configuration_change', 'profile_and_configuration_change')),
    ADD CONSTRAINT project_video_production_rebuilds_target_configuration_check
        CHECK (jsonb_typeof(target_configuration) = 'object'),
    ADD CONSTRAINT project_video_production_rebuilds_target_configuration_hash_check
        CHECK (target_configuration_hash IS NULL OR target_configuration_hash ~ '^[0-9a-f]{64}$');

ALTER TABLE projects
    DROP CONSTRAINT projects_video_production_state_check,
    ADD CONSTRAINT projects_video_production_state_check
        CHECK (video_production_state IN ('unconfigured', 'storyboard_required', 'ready', 'rebuilding', 'blocked', 'reconfiguration_required'));

UPDATE projects project
SET video_production_state = 'reconfiguration_required', updated_at = now()
FROM project_video_production_generations generation
JOIN project_video_production_bindings binding ON binding.id = generation.binding_id
WHERE project.active_video_production_generation_id = generation.id
  AND generation.status = 'active'
  AND binding.status = 'active'
  AND project.video_production_locked = false
  AND CASE
        WHEN COALESCE(binding.profile_snapshot->>'schemaVersion', '') ~ '^[0-9]+$'
          THEN (binding.profile_snapshot->>'schemaVersion')::integer
        ELSE 0
      END < 2;

-- +goose Down

SET search_path TO public;

UPDATE projects
SET video_production_state = 'blocked', updated_at = now()
WHERE video_production_state = 'reconfiguration_required';

ALTER TABLE projects
    DROP CONSTRAINT projects_video_production_state_check,
    ADD CONSTRAINT projects_video_production_state_check
        CHECK (video_production_state IN ('unconfigured', 'storyboard_required', 'ready', 'rebuilding', 'blocked'));

ALTER TABLE project_video_production_rebuilds
    DROP CONSTRAINT IF EXISTS project_video_production_rebuilds_target_configuration_hash_check,
    DROP CONSTRAINT IF EXISTS project_video_production_rebuilds_target_configuration_check,
    DROP CONSTRAINT IF EXISTS project_video_production_rebuilds_reason_check,
    DROP COLUMN IF EXISTS target_configuration_hash,
    DROP COLUMN IF EXISTS target_configuration,
    DROP COLUMN IF EXISTS reason;
