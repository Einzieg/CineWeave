-- +goose Up

SET search_path TO public;

-- Projects that were automatically translated from the legacy production_mode
-- are intentionally left unconfigured. New projects and explicit rebuilds write
-- a binding/generation atomically through the application service.
ALTER TABLE projects
    ALTER COLUMN active_video_production_generation_id DROP NOT NULL,
    ALTER COLUMN video_production_generation_no DROP NOT NULL,
    DROP CONSTRAINT projects_video_production_state_check,
    ADD CONSTRAINT projects_video_production_state_check CHECK (
        video_production_state IN ('unconfigured', 'storyboard_required', 'ready', 'rebuilding', 'blocked')
    );

CREATE TEMP TABLE legacy_auto_configured_projects(
    project_id UUID PRIMARY KEY,
    active_generation_id UUID NOT NULL,
    active_binding_id UUID NOT NULL
) ON COMMIT DROP;

INSERT INTO legacy_auto_configured_projects(project_id, active_generation_id, active_binding_id)
SELECT project.id, active_generation.id, active_generation.binding_id
FROM projects project
JOIN project_video_production_generations active_generation
  ON active_generation.id = project.active_video_production_generation_id
JOIN project_video_production_bindings active_binding
  ON active_binding.id = active_generation.binding_id
LEFT JOIN project_video_production_generations source_generation
  ON source_generation.id = active_generation.source_generation_id
LEFT JOIN project_video_production_bindings source_binding
  ON source_binding.id = source_generation.binding_id
WHERE active_generation.status = 'active'
  AND (
      COALESCE(active_binding.profile_snapshot->>'migrationState', '') LIKE 'legacy_video_data%'
      OR COALESCE(source_binding.profile_snapshot->>'migrationState', '') LIKE 'legacy_video_data%'
  );

-- Existing v12 plans were compiled without a distinct continuation contract.
-- Fence-aware archival must happen while their generation is still active.
ALTER TABLE video_render_segments DISABLE TRIGGER video_render_segments_active_generation_guard;
ALTER TABLE video_render_plans DISABLE TRIGGER video_render_plans_active_generation_guard;

UPDATE video_render_segments
SET status = CASE WHEN status = 'cancelled' THEN status ELSE 'stale' END,
    updated_at = now()
WHERE video_render_plan_id IN (SELECT id FROM video_render_plans);

UPDATE video_render_plans
SET active = false,
    status = CASE WHEN status IN ('cancelled', 'failed', 'archived') THEN status ELSE 'stale' END,
    updated_at = now(),
    metadata = metadata || jsonb_build_object(
        'staleReason', 'continuation_input_contract_required',
        'staleAt', now()
    );

ALTER TABLE video_render_plans ENABLE TRIGGER video_render_plans_active_generation_guard;
ALTER TABLE video_render_segments ENABLE TRIGGER video_render_segments_active_generation_guard;

UPDATE projects project
SET active_video_production_generation_id = NULL,
    video_production_generation_no = NULL,
    video_production_state = 'unconfigured',
    video_production_locked = false,
    updated_at = now()
FROM legacy_auto_configured_projects legacy
WHERE project.id = legacy.project_id;

UPDATE project_video_production_generations generation
SET status = 'superseded', superseded_at = COALESCE(superseded_at, now())
FROM legacy_auto_configured_projects legacy
WHERE generation.id = legacy.active_generation_id;

UPDATE project_video_production_bindings binding
SET status = 'superseded', superseded_at = COALESCE(superseded_at, now())
FROM legacy_auto_configured_projects legacy
WHERE binding.id = legacy.active_binding_id;

-- +goose StatementBegin
CREATE FUNCTION enforce_project_video_production_configuration()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    current_state TEXT;
    current_generation_id UUID;
    current_generation_no BIGINT;
BEGIN
    SELECT video_production_state,
           active_video_production_generation_id,
           video_production_generation_no
    INTO current_state, current_generation_id, current_generation_no
    FROM projects
    WHERE id = NEW.id;

    IF NOT FOUND THEN
        RETURN NEW;
    END IF;

    IF current_state = 'unconfigured' THEN
        IF current_generation_id IS NOT NULL OR current_generation_no IS NOT NULL THEN
            RAISE EXCEPTION 'unconfigured project % cannot have an active video production generation', NEW.id;
        END IF;
    ELSIF current_generation_id IS NULL OR current_generation_no IS NULL THEN
        RAISE EXCEPTION 'configured project % requires an active video production generation', NEW.id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER projects_video_production_configuration_guard
AFTER INSERT OR UPDATE OF video_production_state, active_video_production_generation_id, video_production_generation_no
ON projects
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_project_video_production_configuration();

INSERT INTO video_production_profile_versions(
    id, profile_id, version, lifecycle_state, implementation_state,
    configuration, capability_requirements, prompt_contract, input_contract_version,
    configuration_hash, prompt_contract_hash, published_at
)
SELECT
    md5('cineweave:video-production-profile:single_frame_i2v:v2')::uuid,
    profile.id,
    2,
    'published',
    'available',
    version.configuration || jsonb_build_object(
        'sameShotSegmentContinuity', jsonb_build_array('video_extension', 'previous_segment_tail'),
        'promptExecutionPolicy', 'approved_plan_only'
    ),
    (version.capability_requirements - 'inputContract') || jsonb_build_object(
        'initialInputContract', 'first_frame',
        'allowedContinuationInputContracts', jsonb_build_array('video_extension', 'first_frame')
    ),
    version.prompt_contract,
    'video-input-contract/v2',
    encode(public.digest(pg_catalog.convert_to((
        version.configuration || jsonb_build_object(
            'sameShotSegmentContinuity', jsonb_build_array('video_extension', 'previous_segment_tail'),
            'promptExecutionPolicy', 'approved_plan_only'
        )
    )::text, 'UTF8'), 'sha256'), 'hex'),
    version.prompt_contract_hash,
    now()
FROM video_production_profiles profile
JOIN video_production_profile_versions version
  ON version.profile_id = profile.id AND version.version = 1
WHERE profile.profile_key = 'single_frame_i2v'
ON CONFLICT (profile_id, version) DO NOTHING;

CREATE TABLE provider_model_capability_attestations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    provider_model_id UUID NOT NULL REFERENCES provider_models(id) ON DELETE CASCADE,
    variant_key TEXT NOT NULL,
    capability_snapshot_hash TEXT NOT NULL,
    verification_status TEXT NOT NULL,
    evidence_type TEXT NOT NULL,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    decision TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    decided_by UUID REFERENCES users(id) ON DELETE SET NULL,
    decided_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    supersedes_attestation_id UUID REFERENCES provider_model_capability_attestations(id) ON DELETE RESTRICT,
    revoked_by UUID REFERENCES users(id) ON DELETE SET NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT provider_model_capability_attestations_variant_check CHECK (btrim(variant_key) <> ''),
    CONSTRAINT provider_model_capability_attestations_hash_check CHECK (
        capability_snapshot_hash ~ '^(sha256:)?[0-9a-f]{64}$'
    ),
    CONSTRAINT provider_model_capability_attestations_verification_check CHECK (
        verification_status IN ('official', 'tested', 'inferred', 'unknown')
    ),
    CONSTRAINT provider_model_capability_attestations_evidence_type_check CHECK (
        evidence_type IN ('official_documentation', 'adapter_contract_test', 'controlled_probe', 'administrator_review')
    ),
    CONSTRAINT provider_model_capability_attestations_evidence_check CHECK (jsonb_typeof(evidence) = 'object'),
    CONSTRAINT provider_model_capability_attestations_decision_check CHECK (decision IN ('approved', 'rejected')),
    CONSTRAINT provider_model_capability_attestations_revocation_check CHECK (
        (revoked_at IS NULL AND revoked_by IS NULL) OR revoked_at IS NOT NULL
    )
);

CREATE INDEX provider_model_capability_attestations_lookup_idx
    ON provider_model_capability_attestations(
        organization_id, provider_model_id, variant_key, capability_snapshot_hash, decided_at DESC, id DESC
    );

CREATE UNIQUE INDEX provider_model_capability_attestations_supersedes_once
    ON provider_model_capability_attestations(supersedes_attestation_id)
    WHERE supersedes_attestation_id IS NOT NULL;
CREATE UNIQUE INDEX provider_model_capability_attestations_one_active
    ON provider_model_capability_attestations(
        organization_id, provider_model_id, variant_key, capability_snapshot_hash
    )
    WHERE revoked_at IS NULL;

ALTER TABLE video_render_plans
    RENAME COLUMN input_contract_snapshot TO initial_input_contract_snapshot;
ALTER TABLE video_render_plans
    RENAME COLUMN input_contract_hash TO initial_input_contract_hash;
ALTER TABLE video_render_plans
    RENAME CONSTRAINT video_render_plans_input_contract_snapshot_check
    TO video_render_plans_initial_input_contract_snapshot_check;
ALTER TABLE video_render_plans
    RENAME CONSTRAINT video_render_plans_input_contract_hash_check
    TO video_render_plans_initial_input_contract_hash_check;

ALTER TABLE video_render_plans
    ADD COLUMN continuation_input_contract_snapshot JSONB,
    ADD COLUMN continuation_input_contract_hash TEXT,
    ADD COLUMN capability_attestation_id UUID REFERENCES provider_model_capability_attestations(id) ON DELETE RESTRICT,
    ADD CONSTRAINT video_render_plans_continuation_contract_pair_check CHECK (
        (continuation_input_contract_snapshot IS NULL) = (continuation_input_contract_hash IS NULL)
    ),
    ADD CONSTRAINT video_render_plans_continuation_contract_snapshot_check CHECK (
        continuation_input_contract_snapshot IS NULL OR jsonb_typeof(continuation_input_contract_snapshot) = 'object'
    ),
    ADD CONSTRAINT video_render_plans_continuation_contract_hash_check CHECK (
        continuation_input_contract_hash IS NULL OR continuation_input_contract_hash ~ '^[0-9a-f]{64}$'
    );

ALTER TABLE video_render_segments
    ADD COLUMN input_contract_key TEXT,
    ADD COLUMN input_contract_hash TEXT,
    ADD COLUMN source_video_prompt_plan_id UUID REFERENCES video_prompt_plans(id) ON DELETE RESTRICT,
    ADD COLUMN source_prompt_hash TEXT,
    ADD CONSTRAINT video_render_segments_contract_hash_check CHECK (
        input_contract_hash IS NULL OR input_contract_hash ~ '^[0-9a-f]{64}$'
    ),
    ADD CONSTRAINT video_render_segments_prompt_hash_check CHECK (
        source_prompt_hash IS NULL OR source_prompt_hash ~ '^[0-9a-f]{64}$'
    ),
    ADD CONSTRAINT video_render_segments_execution_contract_required CHECK (
        status IN ('stale', 'cancelled')
        OR (
            input_contract_key IS NOT NULL
            AND input_contract_hash IS NOT NULL
            AND source_video_prompt_plan_id IS NOT NULL
            AND source_prompt_hash IS NOT NULL
        )
    );

CREATE INDEX video_render_segments_execution_contract_idx
    ON video_render_segments(video_render_plan_id, segment_index, input_contract_key, status);

CREATE TABLE episode_video_production_checkpoints (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    production_generation_id UUID NOT NULL,
    video_production_binding_id UUID NOT NULL,
    video_production_binding_revision BIGINT NOT NULL,
    script_episode_id UUID NOT NULL REFERENCES script_episodes(id) ON DELETE CASCADE,
    profile_version_id UUID NOT NULL REFERENCES video_production_profile_versions(id) ON DELETE RESTRICT,
    profile_snapshot_hash TEXT NOT NULL,
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    temporal_workflow_id TEXT NOT NULL,
    temporal_run_id TEXT,
    status TEXT NOT NULL DEFAULT 'queued',
    next_batch_ordinal INTEGER NOT NULL DEFAULT 0,
    revision BIGINT NOT NULL DEFAULT 1,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT episode_video_production_checkpoints_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE CASCADE,
    CONSTRAINT episode_video_production_checkpoints_binding_fk
        FOREIGN KEY (video_production_binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id) ON DELETE CASCADE,
    CONSTRAINT episode_video_production_checkpoints_binding_revision_check CHECK (video_production_binding_revision > 0),
    CONSTRAINT episode_video_production_checkpoints_profile_hash_check CHECK (profile_snapshot_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT episode_video_production_checkpoints_status_check CHECK (
        status IN ('queued', 'running', 'partial_succeeded', 'succeeded', 'failed', 'cancelling', 'cancelled')
    ),
    CONSTRAINT episode_video_production_checkpoints_batch_ordinal_check CHECK (next_batch_ordinal >= 0),
    CONSTRAINT episode_video_production_checkpoints_revision_check CHECK (revision > 0),
    CONSTRAINT episode_video_production_checkpoints_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE UNIQUE INDEX episode_video_production_checkpoints_one_active
    ON episode_video_production_checkpoints(project_id, production_generation_id, script_episode_id)
    WHERE status IN ('queued', 'running', 'cancelling');

CREATE INDEX episode_video_production_checkpoints_status_idx
    ON episode_video_production_checkpoints(project_id, production_generation_id, status, updated_at DESC);

CREATE TRIGGER episode_video_production_checkpoints_set_updated_at
BEFORE UPDATE ON episode_video_production_checkpoints
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE episode_video_production_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    checkpoint_id UUID NOT NULL REFERENCES episode_video_production_checkpoints(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL,
    dependency_snapshot_hash TEXT NOT NULL,
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    temporal_workflow_id TEXT,
    temporal_run_id TEXT,
    status TEXT NOT NULL DEFAULT 'queued',
    attempt INTEGER NOT NULL DEFAULT 1,
    total_items INTEGER NOT NULL DEFAULT 0,
    succeeded_items INTEGER NOT NULL DEFAULT 0,
    failed_items INTEGER NOT NULL DEFAULT 0,
    cancelled_items INTEGER NOT NULL DEFAULT 0,
    revision BIGINT NOT NULL DEFAULT 1,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT episode_video_production_batches_ordinal_check CHECK (ordinal >= 0),
    CONSTRAINT episode_video_production_batches_dependency_hash_check CHECK (dependency_snapshot_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT episode_video_production_batches_status_check CHECK (
        status IN ('queued', 'running', 'partial_succeeded', 'succeeded', 'failed', 'cancelling', 'cancelled')
    ),
    CONSTRAINT episode_video_production_batches_attempt_check CHECK (attempt > 0),
    CONSTRAINT episode_video_production_batches_counts_check CHECK (
        total_items >= 0 AND succeeded_items >= 0 AND failed_items >= 0 AND cancelled_items >= 0
        AND succeeded_items + failed_items + cancelled_items <= total_items
    ),
    CONSTRAINT episode_video_production_batches_revision_check CHECK (revision > 0),
    CONSTRAINT episode_video_production_batches_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    UNIQUE(checkpoint_id, ordinal, attempt)
);

CREATE INDEX episode_video_production_batches_status_idx
    ON episode_video_production_batches(checkpoint_id, status, ordinal);

CREATE TRIGGER episode_video_production_batches_set_updated_at
BEFORE UPDATE ON episode_video_production_batches
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE episode_video_production_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id UUID NOT NULL REFERENCES episode_video_production_batches(id) ON DELETE CASCADE,
    storyboard_shot_id UUID NOT NULL REFERENCES storyboard_shots(id) ON DELETE CASCADE,
    shot_state_hash TEXT NOT NULL,
    reference_pack_id UUID REFERENCES shot_reference_packs(id) ON DELETE SET NULL,
    video_prompt_plan_id UUID REFERENCES video_prompt_plans(id) ON DELETE SET NULL,
    video_render_plan_id UUID REFERENCES video_render_plans(id) ON DELETE SET NULL,
    provider_async_task_id UUID REFERENCES provider_async_tasks(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    attempt INTEGER NOT NULL DEFAULT 1,
    revision BIGINT NOT NULL DEFAULT 1,
    error_code TEXT,
    error_detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT episode_video_production_items_shot_hash_check CHECK (shot_state_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT episode_video_production_items_status_check CHECK (
        status IN ('queued', 'running', 'succeeded', 'failed', 'cancelling', 'cancelled', 'discarded')
    ),
    CONSTRAINT episode_video_production_items_attempt_check CHECK (attempt > 0),
    CONSTRAINT episode_video_production_items_revision_check CHECK (revision > 0),
    CONSTRAINT episode_video_production_items_error_check CHECK (jsonb_typeof(error_detail) = 'object'),
    CONSTRAINT episode_video_production_items_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    UNIQUE(batch_id, storyboard_shot_id, attempt)
);

CREATE INDEX episode_video_production_items_ready_idx
    ON episode_video_production_items(batch_id, status, storyboard_shot_id);

CREATE TRIGGER episode_video_production_items_set_updated_at
BEFORE UPDATE ON episode_video_production_items
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down

SET search_path TO public;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM provider_model_capability_attestations)
       OR EXISTS (SELECT 1 FROM episode_video_production_checkpoints)
       OR EXISTS (
           SELECT 1
           FROM project_video_production_bindings
           WHERE profile_version_id = md5('cineweave:video-production-profile:single_frame_i2v:v2')::uuid
       ) THEN
        RAISE EXCEPTION 'cannot roll back video execution contracts after v2 binding, attestation, or checkpoint data exists';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS episode_video_production_items_set_updated_at ON episode_video_production_items;
DROP TABLE IF EXISTS episode_video_production_items;
DROP TRIGGER IF EXISTS episode_video_production_batches_set_updated_at ON episode_video_production_batches;
DROP TABLE IF EXISTS episode_video_production_batches;
DROP TRIGGER IF EXISTS episode_video_production_checkpoints_set_updated_at ON episode_video_production_checkpoints;
DROP TABLE IF EXISTS episode_video_production_checkpoints;

DROP INDEX IF EXISTS video_render_segments_execution_contract_idx;
ALTER TABLE video_render_segments
    DROP CONSTRAINT IF EXISTS video_render_segments_execution_contract_required,
    DROP CONSTRAINT IF EXISTS video_render_segments_prompt_hash_check,
    DROP CONSTRAINT IF EXISTS video_render_segments_contract_hash_check,
    DROP COLUMN IF EXISTS source_prompt_hash,
    DROP COLUMN IF EXISTS source_video_prompt_plan_id,
    DROP COLUMN IF EXISTS input_contract_hash,
    DROP COLUMN IF EXISTS input_contract_key;

ALTER TABLE video_render_plans
    DROP CONSTRAINT IF EXISTS video_render_plans_continuation_contract_hash_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_continuation_contract_snapshot_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_continuation_contract_pair_check,
    DROP COLUMN IF EXISTS capability_attestation_id,
    DROP COLUMN IF EXISTS continuation_input_contract_hash,
    DROP COLUMN IF EXISTS continuation_input_contract_snapshot;

ALTER TABLE video_render_plans
    RENAME CONSTRAINT video_render_plans_initial_input_contract_snapshot_check
    TO video_render_plans_input_contract_snapshot_check;
ALTER TABLE video_render_plans
    RENAME CONSTRAINT video_render_plans_initial_input_contract_hash_check
    TO video_render_plans_input_contract_hash_check;
ALTER TABLE video_render_plans
    RENAME COLUMN initial_input_contract_snapshot TO input_contract_snapshot;
ALTER TABLE video_render_plans
    RENAME COLUMN initial_input_contract_hash TO input_contract_hash;

DROP INDEX IF EXISTS provider_model_capability_attestations_supersedes_once;
DROP INDEX IF EXISTS provider_model_capability_attestations_one_active;
DROP INDEX IF EXISTS provider_model_capability_attestations_lookup_idx;
DROP TABLE IF EXISTS provider_model_capability_attestations;

DROP TRIGGER IF EXISTS projects_video_production_configuration_guard ON projects;
DROP FUNCTION IF EXISTS enforce_project_video_production_configuration();

DELETE FROM video_production_profile_versions
WHERE id = md5('cineweave:video-production-profile:single_frame_i2v:v2')::uuid;

CREATE TEMP TABLE rollback_unconfigured_projects(
    project_id UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    created_by UUID,
    profile_version_id UUID NOT NULL,
    snapshot JSONB NOT NULL,
    snapshot_hash TEXT NOT NULL,
    next_binding_revision BIGINT NOT NULL,
    next_generation_no BIGINT NOT NULL,
    source_generation_id UUID
) ON COMMIT DROP;

INSERT INTO rollback_unconfigured_projects(
    project_id, organization_id, created_by, profile_version_id,
    snapshot, snapshot_hash, next_binding_revision, next_generation_no, source_generation_id
)
SELECT candidate.project_id,
       candidate.organization_id,
       candidate.created_by,
       candidate.profile_version_id,
       candidate.snapshot,
       encode(public.digest(pg_catalog.convert_to(candidate.snapshot::text, 'UTF8'), 'sha256'), 'hex'),
       candidate.next_binding_revision,
       candidate.next_generation_no,
       candidate.source_generation_id
FROM (
    SELECT project.id AS project_id,
           project.organization_id,
           project.created_by,
           version.id AS profile_version_id,
           jsonb_build_object(
               'profileKey', profile.profile_key,
               'profileName', profile.name,
               'profileVersion', version.version,
               'profileVersionId', version.id,
               'configuration', version.configuration,
               'capabilityRequirements', version.capability_requirements,
               'promptContract', version.prompt_contract,
               'inputContractVersion', version.input_contract_version,
               'configurationHash', version.configuration_hash,
               'promptContractHash', version.prompt_contract_hash,
               'migrationState', 'legacy_video_data_v13_rollback'
           ) AS snapshot,
           COALESCE((
               SELECT max(binding.revision) + 1
               FROM project_video_production_bindings binding
               WHERE binding.project_id = project.id
           ), 1) AS next_binding_revision,
           COALESCE((
               SELECT max(generation.generation_no) + 1
               FROM project_video_production_generations generation
               WHERE generation.project_id = project.id
           ), 1) AS next_generation_no,
           (
               SELECT generation.id
               FROM project_video_production_generations generation
               WHERE generation.project_id = project.id
               ORDER BY generation.generation_no DESC
               LIMIT 1
           ) AS source_generation_id
    FROM projects project
    JOIN video_production_profiles profile ON profile.profile_key = 'single_frame_i2v'
    JOIN LATERAL (
        SELECT candidate_version.*
        FROM video_production_profile_versions candidate_version
        WHERE candidate_version.profile_id = profile.id
          AND candidate_version.version = 1
    ) version ON true
    WHERE project.video_production_state = 'unconfigured'
) candidate;

INSERT INTO project_video_production_bindings(
    project_id, profile_version_id, status, compatibility_policy, overrides,
    profile_snapshot, profile_snapshot_hash, revision, created_by
)
SELECT project_id, profile_version_id, 'active', 'strict', '{}'::jsonb,
       snapshot, snapshot_hash, next_binding_revision, created_by
FROM rollback_unconfigured_projects;

INSERT INTO project_video_production_generations(
    organization_id, project_id, binding_id, generation_no, status,
    source_generation_id, activated_at
)
SELECT rollback.organization_id,
       rollback.project_id,
       binding.id,
       rollback.next_generation_no,
       'active',
       rollback.source_generation_id,
       now()
FROM rollback_unconfigured_projects rollback
JOIN project_video_production_bindings binding
  ON binding.project_id = rollback.project_id
 AND binding.revision = rollback.next_binding_revision
 AND binding.status = 'active';

UPDATE projects project
SET active_video_production_generation_id = generation.id,
    video_production_generation_no = generation.generation_no,
    video_production_state = 'storyboard_required',
    updated_at = now()
FROM project_video_production_generations generation
WHERE generation.project_id = project.id
  AND generation.status = 'active'
  AND project.video_production_state = 'unconfigured';

SET CONSTRAINTS ALL IMMEDIATE;

ALTER TABLE projects
    DROP CONSTRAINT projects_video_production_state_check,
    ADD CONSTRAINT projects_video_production_state_check CHECK (
        video_production_state IN ('storyboard_required', 'ready', 'rebuilding', 'blocked')
    ),
    ALTER COLUMN active_video_production_generation_id SET NOT NULL,
    ALTER COLUMN video_production_generation_no SET NOT NULL;
