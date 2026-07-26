-- +goose Up

SET search_path TO public;

ALTER TABLE commerce_script_unit_generations
    ADD COLUMN storyboard_strategy TEXT
        GENERATED ALWAYS AS (NULLIF(unit_configuration_snapshot->>'storyboardStrategy', '')) STORED,
    ADD COLUMN segmentation_policy_version TEXT
        GENERATED ALWAYS AS (NULLIF(unit_configuration_snapshot->>'segmentationPolicyVersion', '')) STORED,
    ADD CONSTRAINT commerce_unit_generations_storyboard_strategy_check CHECK (
        storyboard_strategy IS NULL
        OR storyboard_strategy IN ('smart', 'single_take', 'manual')
    ),
    ADD CONSTRAINT commerce_unit_generations_segmentation_policy_check CHECK (
        segmentation_policy_version IS NULL
        OR trim(segmentation_policy_version) <> ''
    ),
    ADD CONSTRAINT commerce_unit_generations_configuration_v3_check CHECK (
        COALESCE(unit_configuration_snapshot->>'schemaVersion', '') <> '3'
        OR (
            storyboard_strategy IN ('smart', 'single_take', 'manual')
            AND segmentation_policy_version IS NOT NULL
        )
    );

CREATE INDEX commerce_unit_generations_strategy_idx
    ON commerce_script_unit_generations(project_id, storyboard_strategy, status)
    WHERE storyboard_strategy IS NOT NULL;

ALTER TABLE commerce_storyboard_plans
    ADD COLUMN segmentation_policy_version TEXT,
    ADD COLUMN segmentation_plan JSONB,
    ADD COLUMN segmentation_plan_hash TEXT,
    ADD COLUMN video_execution_envelope JSONB,
    ADD COLUMN video_execution_envelope_hash TEXT,
    ADD COLUMN timing_advisory JSONB,
    ADD COLUMN preview_hash TEXT,
    ADD CONSTRAINT commerce_storyboard_plans_segmentation_contract_check CHECK (
        (
            segmentation_policy_version IS NULL
            AND segmentation_plan IS NULL
            AND segmentation_plan_hash IS NULL
            AND video_execution_envelope IS NULL
            AND video_execution_envelope_hash IS NULL
            AND timing_advisory IS NULL
            AND preview_hash IS NULL
        )
        OR (
            trim(segmentation_policy_version) <> ''
            AND jsonb_typeof(segmentation_plan) = 'object'
            AND segmentation_plan_hash ~ '^[0-9a-f]{64}$'
            AND jsonb_typeof(video_execution_envelope) = 'object'
            AND video_execution_envelope_hash ~ '^[0-9a-f]{64}$'
            AND jsonb_typeof(timing_advisory) = 'object'
            AND preview_hash ~ '^[0-9a-f]{64}$'
        )
    );

CREATE INDEX commerce_storyboard_plans_preview_hash_idx
    ON commerce_storyboard_plans(script_unit_generation_id, preview_hash)
    WHERE preview_hash IS NOT NULL;

CREATE TABLE commerce_storyboard_preview_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    commerce_script_unit_id UUID NOT NULL REFERENCES commerce_script_units(id) ON DELETE CASCADE,
    script_unit_generation_id UUID NOT NULL REFERENCES commerce_script_unit_generations(id) ON DELETE CASCADE,
    project_production_generation_id UUID NOT NULL REFERENCES project_video_production_generations(id) ON DELETE CASCADE,
    script_unit_revision BIGINT NOT NULL,
    client_request_id UUID NOT NULL,
    input_hash TEXT NOT NULL,
    preview_hash TEXT NOT NULL,
    video_execution_envelope_hash TEXT NOT NULL,
    segmentation_plan_hash TEXT NOT NULL,
    preview_snapshot JSONB NOT NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_storyboard_preview_attempts_revision_check
        CHECK (script_unit_revision > 0),
    CONSTRAINT commerce_storyboard_preview_attempts_hashes_check CHECK (
        input_hash ~ '^[0-9a-f]{64}$'
        AND preview_hash ~ '^[0-9a-f]{64}$'
        AND video_execution_envelope_hash ~ '^[0-9a-f]{64}$'
        AND segmentation_plan_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT commerce_storyboard_preview_attempts_snapshot_check
        CHECK (jsonb_typeof(preview_snapshot) = 'object'),
    UNIQUE(script_unit_generation_id, client_request_id)
);

CREATE INDEX commerce_storyboard_preview_attempts_generation_idx
    ON commerce_storyboard_preview_attempts(
        script_unit_generation_id,
        script_unit_revision,
        created_at DESC
    );

ALTER TABLE commerce_shot_contracts
    ADD COLUMN creative_direction JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN estimated_voiceover_ticks BIGINT,
    ADD COLUMN voiceover_overflow_ticks BIGINT,
    ADD COLUMN timing_advisory_level TEXT,
    ADD COLUMN recommended_request_duration_seconds INTEGER,
    ADD COLUMN eligible_route_set_hash TEXT,
    ADD CONSTRAINT commerce_shot_contracts_creative_direction_check
        CHECK (jsonb_typeof(creative_direction) = 'object'),
    ADD CONSTRAINT commerce_shot_contracts_voiceover_ticks_check
        CHECK (estimated_voiceover_ticks IS NULL OR estimated_voiceover_ticks >= 0),
    ADD CONSTRAINT commerce_shot_contracts_voiceover_overflow_check
        CHECK (voiceover_overflow_ticks IS NULL OR voiceover_overflow_ticks >= 0),
    ADD CONSTRAINT commerce_shot_contracts_timing_advisory_check
        CHECK (
            timing_advisory_level IS NULL
            OR timing_advisory_level IN ('none', 'info', 'warning', 'critical')
        ),
    ADD CONSTRAINT commerce_shot_contracts_request_duration_check
        CHECK (
            recommended_request_duration_seconds IS NULL
            OR recommended_request_duration_seconds > 0
        ),
    ADD CONSTRAINT commerce_shot_contracts_route_set_hash_check
        CHECK (
            eligible_route_set_hash IS NULL
            OR eligible_route_set_hash ~ '^[0-9a-f]{64}$'
        );

-- Existing plans were authored before deterministic segmentation and do not
-- have a frozen execution envelope. They must be rebuilt instead of silently
-- executing against current provider capabilities.
UPDATE commerce_storyboard_plans
SET status = CASE WHEN status = 'archived' THEN status ELSE 'stale' END,
    active = false,
    stale_state = CASE WHEN status = 'archived' THEN stale_state ELSE 'needs_regeneration' END,
    stale_at = CASE WHEN status = 'archived' THEN stale_at ELSE COALESCE(stale_at, now()) END
WHERE segmentation_plan_hash IS NULL;

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_storyboard_segmentation_snapshot()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status IN ('ready', 'stale', 'archived')
       AND (
           NEW.segmentation_policy_version IS DISTINCT FROM OLD.segmentation_policy_version
           OR NEW.segmentation_plan IS DISTINCT FROM OLD.segmentation_plan
           OR NEW.segmentation_plan_hash IS DISTINCT FROM OLD.segmentation_plan_hash
           OR NEW.video_execution_envelope IS DISTINCT FROM OLD.video_execution_envelope
           OR NEW.video_execution_envelope_hash IS DISTINCT FROM OLD.video_execution_envelope_hash
           OR NEW.timing_advisory IS DISTINCT FROM OLD.timing_advisory
           OR NEW.preview_hash IS DISTINCT FROM OLD.preview_hash
       ) THEN
        RAISE EXCEPTION 'reviewed commerce storyboard segmentation snapshots are immutable'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_storyboard_segmentation_snapshot_immutable
BEFORE UPDATE ON commerce_storyboard_plans
FOR EACH ROW EXECUTE FUNCTION protect_commerce_storyboard_segmentation_snapshot();

-- +goose Down

SET search_path TO public;

DROP TRIGGER IF EXISTS commerce_storyboard_segmentation_snapshot_immutable
    ON commerce_storyboard_plans;
DROP FUNCTION IF EXISTS protect_commerce_storyboard_segmentation_snapshot();

ALTER TABLE commerce_shot_contracts
    DROP CONSTRAINT IF EXISTS commerce_shot_contracts_route_set_hash_check,
    DROP CONSTRAINT IF EXISTS commerce_shot_contracts_request_duration_check,
    DROP CONSTRAINT IF EXISTS commerce_shot_contracts_timing_advisory_check,
    DROP CONSTRAINT IF EXISTS commerce_shot_contracts_voiceover_overflow_check,
    DROP CONSTRAINT IF EXISTS commerce_shot_contracts_voiceover_ticks_check,
    DROP CONSTRAINT IF EXISTS commerce_shot_contracts_creative_direction_check,
    DROP COLUMN IF EXISTS eligible_route_set_hash,
    DROP COLUMN IF EXISTS recommended_request_duration_seconds,
    DROP COLUMN IF EXISTS timing_advisory_level,
    DROP COLUMN IF EXISTS voiceover_overflow_ticks,
    DROP COLUMN IF EXISTS estimated_voiceover_ticks,
    DROP COLUMN IF EXISTS creative_direction;

DROP TABLE IF EXISTS commerce_storyboard_preview_attempts;

DROP INDEX IF EXISTS commerce_storyboard_plans_preview_hash_idx;

ALTER TABLE commerce_storyboard_plans
    DROP CONSTRAINT IF EXISTS commerce_storyboard_plans_segmentation_contract_check,
    DROP COLUMN IF EXISTS preview_hash,
    DROP COLUMN IF EXISTS timing_advisory,
    DROP COLUMN IF EXISTS video_execution_envelope_hash,
    DROP COLUMN IF EXISTS video_execution_envelope,
    DROP COLUMN IF EXISTS segmentation_plan_hash,
    DROP COLUMN IF EXISTS segmentation_plan,
    DROP COLUMN IF EXISTS segmentation_policy_version;

DROP INDEX IF EXISTS commerce_unit_generations_strategy_idx;

ALTER TABLE commerce_script_unit_generations
    DROP CONSTRAINT IF EXISTS commerce_unit_generations_configuration_v3_check,
    DROP CONSTRAINT IF EXISTS commerce_unit_generations_segmentation_policy_check,
    DROP CONSTRAINT IF EXISTS commerce_unit_generations_storyboard_strategy_check,
    DROP COLUMN IF EXISTS segmentation_policy_version,
    DROP COLUMN IF EXISTS storyboard_strategy;
