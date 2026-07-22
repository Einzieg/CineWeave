-- +goose Up

SET search_path TO public;

CREATE TABLE video_production_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    strategy_family TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT video_production_profiles_key_check CHECK (profile_key ~ '^[a-z][a-z0-9_]*$'),
    CONSTRAINT video_production_profiles_strategy_check CHECK (btrim(strategy_family) <> '')
);

CREATE TRIGGER video_production_profiles_set_updated_at
BEFORE UPDATE ON video_production_profiles
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE video_production_profile_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id UUID NOT NULL REFERENCES video_production_profiles(id) ON DELETE RESTRICT,
    version INTEGER NOT NULL,
    lifecycle_state TEXT NOT NULL,
    implementation_state TEXT NOT NULL,
    configuration JSONB NOT NULL DEFAULT '{}'::jsonb,
    capability_requirements JSONB NOT NULL DEFAULT '{}'::jsonb,
    prompt_contract JSONB NOT NULL DEFAULT '{}'::jsonb,
    input_contract_version TEXT NOT NULL,
    configuration_hash TEXT NOT NULL,
    prompt_contract_hash TEXT NOT NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    retired_at TIMESTAMPTZ,
    CONSTRAINT video_production_profile_versions_version_check CHECK (version > 0),
    CONSTRAINT video_production_profile_versions_lifecycle_check CHECK (lifecycle_state IN ('draft', 'published', 'retired')),
    CONSTRAINT video_production_profile_versions_implementation_check CHECK (implementation_state IN ('reserved', 'available')),
    CONSTRAINT video_production_profile_versions_configuration_check CHECK (jsonb_typeof(configuration) = 'object'),
    CONSTRAINT video_production_profile_versions_capability_check CHECK (jsonb_typeof(capability_requirements) = 'object'),
    CONSTRAINT video_production_profile_versions_prompt_contract_check CHECK (jsonb_typeof(prompt_contract) = 'object'),
    CONSTRAINT video_production_profile_versions_configuration_hash_check CHECK (configuration_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT video_production_profile_versions_prompt_hash_check CHECK (prompt_contract_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT video_production_profile_versions_publication_check CHECK (
        (lifecycle_state = 'draft' AND published_at IS NULL AND retired_at IS NULL)
        OR (lifecycle_state = 'published' AND published_at IS NOT NULL AND retired_at IS NULL)
        OR (lifecycle_state = 'retired' AND published_at IS NOT NULL AND retired_at IS NOT NULL)
    ),
    UNIQUE(profile_id, version)
);

-- +goose StatementBegin
CREATE FUNCTION protect_video_production_profile_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.lifecycle_state = 'retired' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'retired video production profile versions are immutable' USING ERRCODE = '55000';
    END IF;

    IF OLD.lifecycle_state = 'published' AND (
        NEW.profile_id IS DISTINCT FROM OLD.profile_id
        OR NEW.version IS DISTINCT FROM OLD.version
        OR NEW.implementation_state IS DISTINCT FROM OLD.implementation_state
        OR NEW.configuration IS DISTINCT FROM OLD.configuration
        OR NEW.capability_requirements IS DISTINCT FROM OLD.capability_requirements
        OR NEW.prompt_contract IS DISTINCT FROM OLD.prompt_contract
        OR NEW.input_contract_version IS DISTINCT FROM OLD.input_contract_version
        OR NEW.configuration_hash IS DISTINCT FROM OLD.configuration_hash
        OR NEW.prompt_contract_hash IS DISTINCT FROM OLD.prompt_contract_hash
        OR NEW.created_by IS DISTINCT FROM OLD.created_by
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
        OR NEW.published_at IS DISTINCT FROM OLD.published_at
        OR NEW.lifecycle_state NOT IN ('published', 'retired')
        OR (NEW.lifecycle_state = 'published' AND NEW.retired_at IS DISTINCT FROM OLD.retired_at)
        OR (NEW.lifecycle_state = 'retired' AND NEW.retired_at IS NULL)
    ) THEN
        RAISE EXCEPTION 'published video production profile versions are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER video_production_profile_versions_immutable
BEFORE UPDATE ON video_production_profile_versions
FOR EACH ROW EXECUTE FUNCTION protect_video_production_profile_version();

CREATE TABLE project_video_production_bindings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    profile_version_id UUID NOT NULL REFERENCES video_production_profile_versions(id) ON DELETE RESTRICT,
    status TEXT NOT NULL,
    compatibility_policy TEXT NOT NULL DEFAULT 'strict',
    overrides JSONB NOT NULL DEFAULT '{}'::jsonb,
    profile_snapshot JSONB NOT NULL,
    profile_snapshot_hash TEXT NOT NULL,
    revision BIGINT NOT NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    superseded_by_rebuild_id UUID,
    superseded_at TIMESTAMPTZ,
    CONSTRAINT project_video_production_bindings_status_check CHECK (status IN ('active', 'superseded')),
    CONSTRAINT project_video_production_bindings_policy_check CHECK (compatibility_policy IN ('strict', 'compatible_fallback')),
    CONSTRAINT project_video_production_bindings_overrides_check CHECK (jsonb_typeof(overrides) = 'object'),
    CONSTRAINT project_video_production_bindings_snapshot_check CHECK (jsonb_typeof(profile_snapshot) = 'object'),
    CONSTRAINT project_video_production_bindings_hash_check CHECK (profile_snapshot_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT project_video_production_bindings_revision_check CHECK (revision > 0),
    CONSTRAINT project_video_production_bindings_superseded_check CHECK (
        (status = 'active' AND superseded_at IS NULL)
        OR (status = 'superseded' AND superseded_at IS NOT NULL)
    ),
    UNIQUE(project_id, revision),
    UNIQUE(id, project_id)
);

CREATE UNIQUE INDEX project_video_production_bindings_one_active
    ON project_video_production_bindings(project_id)
    WHERE status = 'active';

-- +goose StatementBegin
CREATE FUNCTION protect_project_video_production_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.profile_version_id IS DISTINCT FROM OLD.profile_version_id
       OR NEW.compatibility_policy IS DISTINCT FROM OLD.compatibility_policy
       OR NEW.overrides IS DISTINCT FROM OLD.overrides
       OR NEW.profile_snapshot IS DISTINCT FROM OLD.profile_snapshot
       OR NEW.profile_snapshot_hash IS DISTINCT FROM OLD.profile_snapshot_hash
       OR NEW.revision IS DISTINCT FROM OLD.revision
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'video production bindings are append-only' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'superseded' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'superseded video production bindings are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'active' AND NEW.status NOT IN ('active', 'superseded') THEN
        RAISE EXCEPTION 'invalid video production binding transition' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_video_production_bindings_append_only
BEFORE UPDATE ON project_video_production_bindings
FOR EACH ROW EXECUTE FUNCTION protect_project_video_production_binding();

CREATE TABLE project_video_production_generations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    binding_id UUID NOT NULL,
    generation_no BIGINT NOT NULL,
    status TEXT NOT NULL,
    source_generation_id UUID,
    rebuild_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ,
    superseded_at TIMESTAMPTZ,
    CONSTRAINT project_video_production_generations_number_check CHECK (generation_no > 0),
    CONSTRAINT project_video_production_generations_status_check CHECK (status IN ('preparing', 'active', 'superseded', 'failed')),
    CONSTRAINT project_video_production_generations_state_check CHECK (
        (status = 'preparing' AND activated_at IS NULL AND superseded_at IS NULL)
        OR (status = 'active' AND activated_at IS NOT NULL AND superseded_at IS NULL)
        OR (status = 'superseded' AND activated_at IS NOT NULL AND superseded_at IS NOT NULL)
        OR (status = 'failed' AND superseded_at IS NULL)
    ),
    UNIQUE(project_id, generation_no),
    UNIQUE(id, project_id),
    CONSTRAINT project_video_production_generations_binding_fk
        FOREIGN KEY (binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT project_video_production_generations_source_fk
        FOREIGN KEY (source_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX project_video_production_generations_one_active
    ON project_video_production_generations(project_id)
    WHERE status = 'active';

CREATE TABLE project_video_production_rebuilds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_binding_id UUID NOT NULL,
    source_generation_id UUID NOT NULL,
    target_profile_version_id UUID NOT NULL REFERENCES video_production_profile_versions(id) ON DELETE RESTRICT,
    target_binding_id UUID,
    target_generation_id UUID,
    status TEXT NOT NULL DEFAULT 'planned',
    impact_snapshot JSONB NOT NULL,
    impact_token TEXT NOT NULL,
    expected_project_revision BIGINT NOT NULL,
    episode_count INTEGER NOT NULL DEFAULT 0,
    retained_asset_count INTEGER NOT NULL DEFAULT 0,
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    idempotency_key TEXT NOT NULL,
    requested_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    approved_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    failure_code TEXT,
    failure_message TEXT,
    CONSTRAINT project_video_production_rebuilds_status_check CHECK (status IN ('planned', 'approved', 'running', 'partial_succeeded', 'storyboard_required', 'succeeded', 'failed', 'cancelled')),
    CONSTRAINT project_video_production_rebuilds_impact_check CHECK (jsonb_typeof(impact_snapshot) = 'object'),
    CONSTRAINT project_video_production_rebuilds_impact_token_check CHECK (impact_token ~ '^[0-9a-f]{64}$'),
    CONSTRAINT project_video_production_rebuilds_revision_check CHECK (expected_project_revision > 0),
    CONSTRAINT project_video_production_rebuilds_episode_count_check CHECK (episode_count >= 0),
    CONSTRAINT project_video_production_rebuilds_asset_count_check CHECK (retained_asset_count >= 0),
    CONSTRAINT project_video_production_rebuilds_source_binding_fk
        FOREIGN KEY (source_binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT project_video_production_rebuilds_source_generation_fk
        FOREIGN KEY (source_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT project_video_production_rebuilds_target_binding_fk
        FOREIGN KEY (target_binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT project_video_production_rebuilds_target_generation_fk
        FOREIGN KEY (target_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    UNIQUE(project_id, idempotency_key),
    UNIQUE(id, project_id)
);

CREATE UNIQUE INDEX project_video_production_rebuilds_one_active
    ON project_video_production_rebuilds(project_id)
    WHERE status IN ('planned', 'approved', 'running', 'partial_succeeded', 'storyboard_required');

ALTER TABLE project_video_production_bindings
    ADD CONSTRAINT project_video_production_bindings_rebuild_fk
        FOREIGN KEY (superseded_by_rebuild_id) REFERENCES project_video_production_rebuilds(id) ON DELETE RESTRICT;

ALTER TABLE project_video_production_generations
    ADD CONSTRAINT project_video_production_generations_rebuild_fk
        FOREIGN KEY (rebuild_id) REFERENCES project_video_production_rebuilds(id) ON DELETE RESTRICT;

CREATE TABLE project_video_production_rebuild_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rebuild_id UUID NOT NULL,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    script_episode_id UUID NOT NULL REFERENCES script_episodes(id) ON DELETE RESTRICT,
    episode_ordinal INTEGER NOT NULL,
    script_episode_revision BIGINT NOT NULL,
    script_episode_content_hash TEXT NOT NULL,
    source_storyboard_plan_id UUID REFERENCES storyboard_plans(id) ON DELETE SET NULL,
    target_storyboard_plan_id UUID REFERENCES storyboard_plans(id) ON DELETE SET NULL,
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    checkpoint JSONB NOT NULL DEFAULT '{}'::jsonb,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    failure_code TEXT,
    failure_message TEXT,
    CONSTRAINT project_video_production_rebuild_items_status_check CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'skipped')),
    CONSTRAINT project_video_production_rebuild_items_ordinal_check CHECK (episode_ordinal > 0),
    CONSTRAINT project_video_production_rebuild_items_revision_check CHECK (script_episode_revision > 0),
    CONSTRAINT project_video_production_rebuild_items_content_hash_check CHECK (script_episode_content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT project_video_production_rebuild_items_checkpoint_check CHECK (jsonb_typeof(checkpoint) = 'object'),
    CONSTRAINT project_video_production_rebuild_items_attempt_check CHECK (attempt_count >= 0),
    CONSTRAINT project_video_production_rebuild_items_rebuild_fk
        FOREIGN KEY (rebuild_id, project_id)
        REFERENCES project_video_production_rebuilds(id, project_id) ON DELETE CASCADE,
    UNIQUE(rebuild_id, script_episode_id)
);

CREATE INDEX project_video_production_rebuild_items_status_idx
    ON project_video_production_rebuild_items(rebuild_id, status, episode_ordinal);

CREATE TABLE project_video_production_rebuild_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rebuild_id UUID NOT NULL,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    attempt_no INTEGER NOT NULL,
    workflow_run_id UUID NOT NULL REFERENCES workflow_runs(id) ON DELETE RESTRICT,
    idempotency_key TEXT NOT NULL,
    retry_failed_only BOOLEAN NOT NULL DEFAULT false,
    status TEXT NOT NULL DEFAULT 'queued',
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    failure_code TEXT,
    failure_message TEXT,
    CONSTRAINT project_video_production_rebuild_attempts_number_check CHECK (attempt_no > 0),
    CONSTRAINT project_video_production_rebuild_attempts_status_check CHECK (status IN ('queued', 'running', 'succeeded', 'partial_succeeded', 'failed', 'cancelled')),
    CONSTRAINT project_video_production_rebuild_attempts_rebuild_fk
        FOREIGN KEY (rebuild_id, project_id)
        REFERENCES project_video_production_rebuilds(id, project_id) ON DELETE CASCADE,
    UNIQUE(rebuild_id, attempt_no),
    UNIQUE(rebuild_id, idempotency_key)
);

CREATE INDEX project_video_production_rebuild_attempts_status_idx
    ON project_video_production_rebuild_attempts(rebuild_id, status, attempt_no DESC);

ALTER TABLE script_episodes
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN content_hash TEXT;

UPDATE script_episodes
SET content_hash = encode(public.digest(pg_catalog.convert_to(content, 'UTF8'), 'sha256'), 'hex');

ALTER TABLE script_episodes
    ALTER COLUMN content_hash SET NOT NULL,
    ADD CONSTRAINT script_episodes_revision_positive CHECK (revision > 0),
    ADD CONSTRAINT script_episodes_content_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$');

-- +goose StatementBegin
CREATE FUNCTION maintain_script_episode_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.content_hash := encode(public.digest(pg_catalog.convert_to(NEW.content, 'UTF8'), 'sha256'), 'hex');
    IF TG_OP = 'UPDATE' AND (
        NEW.content IS DISTINCT FROM OLD.content
        OR NEW.episode_title IS DISTINCT FROM OLD.episode_title
        OR NEW.episode_index IS DISTINCT FROM OLD.episode_index
        OR NEW.script_version_id IS DISTINCT FROM OLD.script_version_id
    ) THEN
        NEW.revision := OLD.revision + 1;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER script_episodes_maintain_revision
BEFORE INSERT OR UPDATE ON script_episodes
FOR EACH ROW EXECUTE FUNCTION maintain_script_episode_revision();

CREATE TEMP TABLE video_production_profile_seed (
    profile_id UUID NOT NULL,
    version_id UUID NOT NULL,
    profile_key TEXT NOT NULL,
    name TEXT NOT NULL,
    strategy_family TEXT NOT NULL,
    description TEXT NOT NULL,
    implementation_state TEXT NOT NULL,
    configuration JSONB NOT NULL,
    capability_requirements JSONB NOT NULL,
    prompt_contract JSONB NOT NULL
) ON COMMIT DROP;

INSERT INTO video_production_profile_seed(
    profile_id, version_id, profile_key, name, strategy_family, description,
    implementation_state, configuration, capability_requirements, prompt_contract
)
VALUES
    (
        '10000000-0000-4000-8000-000000000001'::uuid,
        '10000000-0000-4000-9000-000000000001'::uuid,
        'single_frame_i2v',
        '图生视频',
        'single_frame',
        '以当前镜头已审核首帧作为视频模型唯一硬视觉输入。',
        'available',
        '{"anchorRoles":["planned_first_frame"],"crossShotTailPolicy":"none","sameShotSegmentContinuity":"previous_segment_tail","requiresAnchorReview":true}'::jsonb,
        '{"taskType":"video.image_to_video","inputContract":"first_frame","maxFirstFrames":{"minimum":1}}'::jsonb,
        '{"anchorPlan":"video_profile.single_frame_i2v.anchor.plan","anchorGenerate":"video_profile.single_frame_i2v.anchor.generate","anchorReview":"video_profile.single_frame_i2v.anchor.review","videoGenerate":"video_profile.single_frame_i2v.video.generate","videoReview":"video_profile.single_frame_i2v.video.review"}'::jsonb
    ),
    (
        '10000000-0000-4000-8000-000000000002'::uuid,
        '10000000-0000-4000-9000-000000000002'::uuid,
        'first_last_frame',
        '首尾帧衔接',
        'first_last_frames',
        '为同一镜头生成并审核计划首帧和计划尾帧。',
        'reserved',
        '{"anchorRoles":["planned_first_frame","planned_last_frame"],"crossShotTailPolicy":"none","requiresAnchorReview":true}'::jsonb,
        '{"taskType":"video.image_to_video","inputContract":"first_last_frames","supportsFirstFrame":true,"supportsLastFrame":true}'::jsonb,
        '{"anchorPlan":"video_profile.first_last_frame.anchor.plan","anchorGenerate":"video_profile.first_last_frame.anchor.generate","anchorReview":"video_profile.first_last_frame.anchor.review","videoGenerate":"video_profile.first_last_frame.video.generate","videoReview":"video_profile.first_last_frame.video.review"}'::jsonb
    ),
    (
        '10000000-0000-4000-8000-000000000003'::uuid,
        '10000000-0000-4000-9000-000000000003'::uuid,
        'multimodal_reference',
        '多模态参考',
        'multimodal_references',
        '使用当前首帧及角色、场景、道具等带类型语义参考。',
        'reserved',
        '{"anchorRoles":["planned_first_frame"],"crossShotTailPolicy":"soft","requiresAnchorReview":true,"deterministicReferencePacking":true}'::jsonb,
        '{"taskType":"video.reference_to_video","inputContract":"first_frame_plus_references","supportsSemanticReferenceImages":true,"maxReferenceImages":{"minimum":1}}'::jsonb,
        '{"anchorPlan":"video_profile.multimodal_reference.anchor.plan","anchorGenerate":"video_profile.multimodal_reference.anchor.generate","anchorReview":"video_profile.multimodal_reference.anchor.review","videoGenerate":"video_profile.multimodal_reference.video.generate","videoReview":"video_profile.multimodal_reference.video.review"}'::jsonb
    ),
    (
        '10000000-0000-4000-8000-000000000004'::uuid,
        '10000000-0000-4000-9000-000000000004'::uuid,
        'storyboard_sheet',
        '分镜板',
        'storyboard_sheet',
        '以同一镜头多个时间点组成的无文字分镜板作为语义参考。',
        'reserved',
        '{"anchorRoles":["storyboard_sheet","storyboard_panel"],"crossShotTailPolicy":"none","requiresAnchorReview":true,"panelManifestRequired":true}'::jsonb,
        '{"taskType":"video.reference_to_video","inputContract":"storyboard_sheet_reference","supportsStoryboardSheetReference":true}'::jsonb,
        '{"anchorPlan":"video_profile.storyboard_sheet.anchor.plan","anchorGenerate":"video_profile.storyboard_sheet.anchor.generate","anchorReview":"video_profile.storyboard_sheet.anchor.review","videoGenerate":"video_profile.storyboard_sheet.video.generate","videoReview":"video_profile.storyboard_sheet.video.review"}'::jsonb
    );

INSERT INTO video_production_profiles(id, profile_key, name, strategy_family, description)
SELECT profile_id, profile_key, name, strategy_family, description
FROM video_production_profile_seed;

INSERT INTO video_production_profile_versions(
    id, profile_id, version, lifecycle_state, implementation_state,
    configuration, capability_requirements, prompt_contract, input_contract_version,
    configuration_hash, prompt_contract_hash, published_at
)
SELECT
    version_id,
    profile_id,
    1,
    'published',
    implementation_state,
    configuration,
    capability_requirements,
    prompt_contract,
    'video-input-contract/v1',
    encode(public.digest(pg_catalog.convert_to(configuration::text, 'UTF8'), 'sha256'), 'hex'),
    encode(public.digest(pg_catalog.convert_to(prompt_contract::text, 'UTF8'), 'sha256'), 'hex'),
    now()
FROM video_production_profile_seed;

ALTER TABLE projects
    ADD COLUMN active_video_production_generation_id UUID,
    ADD COLUMN video_production_generation_no BIGINT,
    ADD COLUMN video_production_state TEXT NOT NULL DEFAULT 'storyboard_required',
    ADD COLUMN video_production_locked BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT projects_video_production_generation_no_check CHECK (video_production_generation_no IS NULL OR video_production_generation_no > 0),
    ADD CONSTRAINT projects_video_production_state_check CHECK (video_production_state IN ('storyboard_required', 'ready', 'rebuilding', 'blocked'));

WITH profile AS (
    SELECT v.id AS profile_version_id, p.profile_key, p.name, v.version,
           v.configuration, v.capability_requirements, v.prompt_contract,
           v.input_contract_version, v.configuration_hash, v.prompt_contract_hash
    FROM video_production_profiles p
    JOIN video_production_profile_versions v ON v.profile_id = p.id
    WHERE p.profile_key = 'single_frame_i2v' AND v.version = 1
), snapshots AS (
    SELECT
        project.id AS project_id,
        project.created_by,
        profile.profile_version_id,
        jsonb_build_object(
            'profileKey', profile.profile_key,
            'profileName', profile.name,
            'profileVersion', profile.version,
            'profileVersionId', profile.profile_version_id,
            'configuration', profile.configuration,
            'capabilityRequirements', profile.capability_requirements,
            'promptContract', profile.prompt_contract,
            'inputContractVersion', profile.input_contract_version,
            'configurationHash', profile.configuration_hash,
            'promptContractHash', profile.prompt_contract_hash,
            'projectOverrides', jsonb_build_object(
                'videoRatio', project.video_ratio,
                'audioStrategy', project.audio_strategy,
                'audioRequirement', project.audio_requirement
            ),
            'manualBindings', COALESCE((
                SELECT jsonb_object_agg(binding.manual_kind, jsonb_build_object(
                    'promptVersionId', binding.prompt_version_id,
                    'bindingId', binding.id
                ) ORDER BY binding.manual_kind)
                FROM project_manual_bindings binding
                WHERE binding.project_id = project.id AND binding.status = 'active'
            ), '{}'::jsonb)
        ) AS snapshot
    FROM projects project
    CROSS JOIN profile
), legacy_bindings AS (
    INSERT INTO project_video_production_bindings(
        project_id, profile_version_id, status, compatibility_policy, overrides,
        profile_snapshot, profile_snapshot_hash, revision, created_by, superseded_at
    )
    SELECT
        project_id,
        profile_version_id,
        'superseded',
        'strict',
        '{}'::jsonb,
        snapshot || '{"migrationState":"legacy_video_data"}'::jsonb,
        encode(public.digest(pg_catalog.convert_to((snapshot || '{"migrationState":"legacy_video_data"}'::jsonb)::text, 'UTF8'), 'sha256'), 'hex'),
        1,
        created_by,
        now()
    FROM snapshots
    RETURNING id, project_id
), active_bindings AS (
    INSERT INTO project_video_production_bindings(
        project_id, profile_version_id, status, compatibility_policy, overrides,
        profile_snapshot, profile_snapshot_hash, revision, created_by
    )
    SELECT
        project_id,
        profile_version_id,
        'active',
        'strict',
        '{}'::jsonb,
        snapshot,
        encode(public.digest(pg_catalog.convert_to(snapshot::text, 'UTF8'), 'sha256'), 'hex'),
        2,
        created_by
    FROM snapshots
    RETURNING id, project_id
), legacy_generations AS (
    INSERT INTO project_video_production_generations(
        organization_id, project_id, binding_id, generation_no, status,
        activated_at, superseded_at
    )
    SELECT project.organization_id, project.id, legacy_bindings.id, 1, 'superseded', project.created_at, now()
    FROM projects project
    JOIN legacy_bindings ON legacy_bindings.project_id = project.id
    RETURNING id, project_id
), active_generations AS (
    INSERT INTO project_video_production_generations(
        organization_id, project_id, binding_id, generation_no, status,
        source_generation_id, activated_at
    )
    SELECT project.organization_id, project.id, active_bindings.id, 2, 'active', legacy_generations.id, now()
    FROM projects project
    JOIN active_bindings ON active_bindings.project_id = project.id
    JOIN legacy_generations ON legacy_generations.project_id = project.id
    RETURNING id, project_id, generation_no
)
UPDATE projects project
SET active_video_production_generation_id = active_generations.id,
    video_production_generation_no = active_generations.generation_no,
    video_production_state = 'storyboard_required',
    active_final_video_version_id = NULL,
    active_audio_mix_version_id = NULL,
    updated_at = now()
FROM active_generations
WHERE project.id = active_generations.project_id;

ALTER TABLE projects
    ALTER COLUMN active_video_production_generation_id SET NOT NULL,
    ALTER COLUMN video_production_generation_no SET NOT NULL,
    ADD CONSTRAINT projects_active_video_production_generation_fk
        FOREIGN KEY (active_video_production_generation_id, id)
        REFERENCES project_video_production_generations(id, project_id)
        ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE workflow_runs ADD COLUMN production_generation_id UUID;
ALTER TABLE storyboard_plans ADD COLUMN production_generation_id UUID;
ALTER TABLE storyboard_shots ADD COLUMN production_generation_id UUID;
ALTER TABLE shot_asset_requirements ADD COLUMN production_generation_id UUID;
ALTER TABLE video_render_plans ADD COLUMN production_generation_id UUID;
ALTER TABLE video_render_segments ADD COLUMN production_generation_id UUID;
ALTER TABLE project_timelines ADD COLUMN production_generation_id UUID;
ALTER TABLE timeline_clips ADD COLUMN production_generation_id UUID;
ALTER TABLE final_video_versions ADD COLUMN production_generation_id UUID;
ALTER TABLE artifacts ADD COLUMN production_generation_id UUID;
ALTER TABLE media_files ADD COLUMN production_generation_id UUID;
ALTER TABLE provider_async_tasks ADD COLUMN production_generation_id UUID;
ALTER TABLE provider_call_logs ADD COLUMN production_generation_id UUID;
ALTER TABLE cost_records ADD COLUMN production_generation_id UUID;

WITH legacy AS (
    SELECT project_id, id AS generation_id
    FROM project_video_production_generations
    WHERE generation_no = 1 AND status = 'superseded'
)
UPDATE workflow_runs target SET production_generation_id = legacy.generation_id
FROM legacy WHERE target.project_id = legacy.project_id;

WITH legacy AS (
    SELECT project_id, id AS generation_id
    FROM project_video_production_generations
    WHERE generation_no = 1 AND status = 'superseded'
)
UPDATE storyboard_plans target
SET production_generation_id = legacy.generation_id,
    status = 'archived', active = false, stale_state = 'needs_regeneration'
FROM legacy WHERE target.project_id = legacy.project_id;

WITH legacy AS (
    SELECT project_id, id AS generation_id
    FROM project_video_production_generations
    WHERE generation_no = 1 AND status = 'superseded'
)
UPDATE storyboard_shots target
SET production_generation_id = legacy.generation_id,
    stale_state = 'needs_regeneration',
    image_status = CASE WHEN image_status = 'running' THEN 'stale' ELSE image_status END,
    video_status = CASE WHEN video_status = 'running' THEN 'stale' ELSE video_status END,
    active_video_render_plan_id = NULL
FROM legacy WHERE target.project_id = legacy.project_id;

WITH legacy AS (
    SELECT project_id, id AS generation_id
    FROM project_video_production_generations
    WHERE generation_no = 1 AND status = 'superseded'
)
UPDATE shot_asset_requirements target
SET production_generation_id = legacy.generation_id,
    stale_state = 'needs_regeneration'
FROM legacy WHERE target.project_id = legacy.project_id;

WITH legacy AS (
    SELECT project_id, id AS generation_id
    FROM project_video_production_generations
    WHERE generation_no = 1 AND status = 'superseded'
)
UPDATE video_render_plans target
SET production_generation_id = legacy.generation_id,
    status = 'archived', active = false
FROM legacy WHERE target.project_id = legacy.project_id;

UPDATE video_render_segments segment
SET production_generation_id = plan.production_generation_id,
    status = CASE WHEN segment.status IN ('planned', 'queued', 'running') THEN 'stale' ELSE segment.status END
FROM video_render_plans plan
WHERE segment.video_render_plan_id = plan.id;

WITH legacy AS (
    SELECT project_id, id AS generation_id
    FROM project_video_production_generations
    WHERE generation_no = 1 AND status = 'superseded'
)
UPDATE project_timelines target
SET production_generation_id = legacy.generation_id,
    status = 'archived', stale_state = 'needs_regeneration'
FROM legacy WHERE target.project_id = legacy.project_id;

UPDATE timeline_clips clip
SET production_generation_id = timeline.production_generation_id,
    stale_state = 'needs_regeneration'
FROM project_timelines timeline
WHERE clip.timeline_id = timeline.id;

WITH legacy AS (
    SELECT project_id, id AS generation_id
    FROM project_video_production_generations
    WHERE generation_no = 1 AND status = 'superseded'
)
UPDATE final_video_versions target
SET production_generation_id = legacy.generation_id,
    status = CASE WHEN status = 'failed' THEN status ELSE 'archived' END
FROM legacy WHERE target.project_id = legacy.project_id;

WITH legacy AS (
    SELECT project_id, id AS generation_id
    FROM project_video_production_generations
    WHERE generation_no = 1 AND status = 'superseded'
), production_artifacts AS (
    SELECT image_artifact_id AS id FROM storyboard_shots WHERE image_artifact_id IS NOT NULL
    UNION SELECT video_artifact_id FROM storyboard_shots WHERE video_artifact_id IS NOT NULL
    UNION SELECT storyboard_artifact_id FROM storyboard_shots WHERE storyboard_artifact_id IS NOT NULL
    UNION SELECT derived_artifact_id FROM shot_asset_requirements WHERE derived_artifact_id IS NOT NULL
    UNION SELECT output_artifact_id FROM video_render_plans WHERE output_artifact_id IS NOT NULL
    UNION SELECT artifact_id FROM video_render_segments WHERE artifact_id IS NOT NULL
    UNION SELECT raw_av_artifact_id FROM video_render_segments WHERE raw_av_artifact_id IS NOT NULL
    UNION SELECT mezzanine_artifact_id FROM video_render_segments WHERE mezzanine_artifact_id IS NOT NULL
    UNION SELECT extracted_audio_artifact_id FROM video_render_segments WHERE extracted_audio_artifact_id IS NOT NULL
    UNION SELECT video_artifact_id FROM timeline_clips WHERE video_artifact_id IS NOT NULL
    UNION SELECT artifact_id FROM final_video_versions WHERE artifact_id IS NOT NULL
)
UPDATE artifacts target
SET production_generation_id = legacy.generation_id
FROM legacy
WHERE target.project_id = legacy.project_id
  AND target.id IN (SELECT id FROM production_artifacts);

WITH legacy AS (
    SELECT project_id, id AS generation_id
    FROM project_video_production_generations
    WHERE generation_no = 1 AND status = 'superseded'
), production_media AS (
    SELECT image_media_file_id AS id FROM storyboard_shots WHERE image_media_file_id IS NOT NULL
    UNION SELECT video_media_file_id FROM storyboard_shots WHERE video_media_file_id IS NOT NULL
    UNION SELECT derived_media_file_id FROM shot_asset_requirements WHERE derived_media_file_id IS NOT NULL
    UNION SELECT output_media_file_id FROM video_render_plans WHERE output_media_file_id IS NOT NULL
    UNION SELECT media_file_id FROM video_render_segments WHERE media_file_id IS NOT NULL
    UNION SELECT video_media_file_id FROM timeline_clips WHERE video_media_file_id IS NOT NULL
    UNION SELECT media_file_id FROM final_video_versions WHERE media_file_id IS NOT NULL
)
UPDATE media_files target
SET production_generation_id = legacy.generation_id
FROM legacy
WHERE target.project_id = legacy.project_id
  AND target.id IN (SELECT id FROM production_media);

WITH legacy AS (
    SELECT project_id, id AS generation_id
    FROM project_video_production_generations
    WHERE generation_no = 1 AND status = 'superseded'
)
UPDATE provider_async_tasks target SET production_generation_id = legacy.generation_id
FROM legacy WHERE target.project_id = legacy.project_id;

WITH legacy AS (
    SELECT project_id, id AS generation_id
    FROM project_video_production_generations
    WHERE generation_no = 1 AND status = 'superseded'
)
UPDATE provider_call_logs target SET production_generation_id = legacy.generation_id
FROM legacy WHERE target.project_id = legacy.project_id;

WITH legacy AS (
    SELECT project_id, id AS generation_id
    FROM project_video_production_generations
    WHERE generation_no = 1 AND status = 'superseded'
)
UPDATE cost_records target SET production_generation_id = legacy.generation_id
FROM legacy WHERE target.project_id = legacy.project_id;

ALTER TABLE workflow_runs
    ALTER COLUMN production_generation_id SET NOT NULL,
    ADD CONSTRAINT workflow_runs_production_generation_fk FOREIGN KEY (production_generation_id, project_id) REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;
ALTER TABLE storyboard_plans
    ALTER COLUMN production_generation_id SET NOT NULL,
    ADD CONSTRAINT storyboard_plans_production_generation_fk FOREIGN KEY (production_generation_id, project_id) REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT storyboard_plans_generation_identity UNIQUE (id, project_id, production_generation_id);
ALTER TABLE storyboard_shots
    ALTER COLUMN production_generation_id SET NOT NULL,
    ADD CONSTRAINT storyboard_shots_production_generation_fk FOREIGN KEY (production_generation_id, project_id) REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT storyboard_shots_generation_identity UNIQUE (id, project_id, production_generation_id);
ALTER TABLE shot_asset_requirements
    ALTER COLUMN production_generation_id SET NOT NULL,
    ADD CONSTRAINT shot_asset_requirements_production_generation_fk FOREIGN KEY (production_generation_id, project_id) REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;
ALTER TABLE video_render_plans
    ALTER COLUMN production_generation_id SET NOT NULL,
    ADD CONSTRAINT video_render_plans_production_generation_fk FOREIGN KEY (production_generation_id, project_id) REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT video_render_plans_generation_identity UNIQUE (id, project_id, production_generation_id);
ALTER TABLE video_render_segments
    ALTER COLUMN production_generation_id SET NOT NULL,
    ADD CONSTRAINT video_render_segments_production_generation_fk FOREIGN KEY (production_generation_id, project_id) REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT video_render_segments_generation_identity UNIQUE (id, project_id, production_generation_id);
ALTER TABLE project_timelines
    ALTER COLUMN production_generation_id SET NOT NULL,
    ADD CONSTRAINT project_timelines_production_generation_fk FOREIGN KEY (production_generation_id, project_id) REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;
ALTER TABLE timeline_clips
    ALTER COLUMN production_generation_id SET NOT NULL,
    ADD CONSTRAINT timeline_clips_production_generation_fk FOREIGN KEY (production_generation_id, project_id) REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;
ALTER TABLE final_video_versions
    ALTER COLUMN production_generation_id SET NOT NULL,
    ADD CONSTRAINT final_video_versions_production_generation_fk FOREIGN KEY (production_generation_id, project_id) REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;
ALTER TABLE artifacts
    ADD CONSTRAINT artifacts_production_generation_fk FOREIGN KEY (production_generation_id, project_id) REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;
ALTER TABLE media_files
    ADD CONSTRAINT media_files_production_generation_fk FOREIGN KEY (production_generation_id, project_id) REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;
ALTER TABLE provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_production_generation_fk FOREIGN KEY (production_generation_id, project_id) REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT provider_async_tasks_video_generation_required CHECK (
        project_id IS NULL OR task_type NOT LIKE 'video.%' OR production_generation_id IS NOT NULL
    );
ALTER TABLE provider_call_logs
    ADD CONSTRAINT provider_call_logs_production_generation_fk FOREIGN KEY (production_generation_id, project_id) REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;
ALTER TABLE cost_records
    ADD CONSTRAINT cost_records_production_generation_fk FOREIGN KEY (production_generation_id, project_id) REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;

CREATE INDEX workflow_runs_generation_idx ON workflow_runs(project_id, production_generation_id, status);
CREATE INDEX storyboard_plans_generation_idx ON storyboard_plans(project_id, production_generation_id, script_episode_id);
CREATE INDEX storyboard_shots_generation_idx ON storyboard_shots(project_id, production_generation_id, episode_index, shot_index);
CREATE INDEX shot_asset_requirements_generation_idx ON shot_asset_requirements(project_id, production_generation_id, storyboard_shot_id);
CREATE INDEX video_render_plans_generation_idx ON video_render_plans(project_id, production_generation_id, storyboard_shot_id, status);
CREATE INDEX provider_async_tasks_generation_idx ON provider_async_tasks(project_id, production_generation_id, status);
CREATE INDEX provider_call_logs_generation_idx ON provider_call_logs(project_id, production_generation_id, created_at DESC);
CREATE INDEX cost_records_generation_idx ON cost_records(project_id, production_generation_id, created_at DESC);

CREATE TABLE storyboard_shot_state_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    production_generation_id UUID NOT NULL,
    storyboard_shot_id UUID NOT NULL,
    state_role TEXT NOT NULL,
    revision INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    state JSONB NOT NULL,
    state_hash TEXT NOT NULL,
    source_type TEXT NOT NULL,
    source_id UUID,
    prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE SET NULL,
    provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    model_id UUID REFERENCES provider_models(id) ON DELETE SET NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    approved_at TIMESTAMPTZ,
    CONSTRAINT storyboard_shot_state_versions_role_check CHECK (state_role IN ('planned_entry', 'planned_exit', 'observed_exit')),
    CONSTRAINT storyboard_shot_state_versions_revision_check CHECK (revision > 0),
    CONSTRAINT storyboard_shot_state_versions_status_check CHECK (status IN ('draft', 'approved', 'rejected', 'stale')),
    CONSTRAINT storyboard_shot_state_versions_state_check CHECK (jsonb_typeof(state) = 'object'),
    CONSTRAINT storyboard_shot_state_versions_hash_check CHECK (state_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT storyboard_shot_state_versions_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT storyboard_shot_state_versions_shot_fk
        FOREIGN KEY (storyboard_shot_id, project_id, production_generation_id)
        REFERENCES storyboard_shots(id, project_id, production_generation_id) ON DELETE CASCADE,
    UNIQUE(storyboard_shot_id, state_role, revision)
);

CREATE UNIQUE INDEX storyboard_shot_state_versions_one_approved
    ON storyboard_shot_state_versions(storyboard_shot_id, state_role)
    WHERE status = 'approved';

CREATE TABLE storyboard_shot_transitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    production_generation_id UUID NOT NULL,
    storyboard_plan_id UUID NOT NULL,
    source_shot_id UUID,
    target_shot_id UUID NOT NULL,
    transition_type TEXT NOT NULL,
    tail_policy TEXT NOT NULL DEFAULT 'none',
    anchor_policy TEXT NOT NULL DEFAULT 'new_anchor',
    carry_constraints JSONB NOT NULL DEFAULT '[]'::jsonb,
    reset_constraints JSONB NOT NULL DEFAULT '[]'::jsonb,
    confidence NUMERIC(5,4) NOT NULL DEFAULT 0,
    revision INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    review_status TEXT NOT NULL DEFAULT 'pending',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT storyboard_shot_transitions_shots_check CHECK (source_shot_id IS NULL OR source_shot_id <> target_shot_id),
    CONSTRAINT storyboard_shot_transitions_type_check CHECK (transition_type IN ('match_action_cut', 'same_scene_cut', 'camera_cut', 'subject_change', 'scene_cut', 'time_jump', 'montage_cut', 'unclassified')),
    CONSTRAINT storyboard_shot_transitions_tail_check CHECK (tail_policy IN ('soft', 'none')),
    CONSTRAINT storyboard_shot_transitions_anchor_check CHECK (anchor_policy IN ('new_anchor', 'match_action_anchor', 'independent_anchor')),
    CONSTRAINT storyboard_shot_transitions_carry_check CHECK (jsonb_typeof(carry_constraints) = 'array'),
    CONSTRAINT storyboard_shot_transitions_reset_check CHECK (jsonb_typeof(reset_constraints) = 'array'),
    CONSTRAINT storyboard_shot_transitions_confidence_check CHECK (confidence >= 0 AND confidence <= 1),
    CONSTRAINT storyboard_shot_transitions_revision_check CHECK (revision > 0),
    CONSTRAINT storyboard_shot_transitions_status_check CHECK (status IN ('active', 'superseded', 'stale')),
    CONSTRAINT storyboard_shot_transitions_review_check CHECK (review_status IN ('pending', 'approved', 'rejected', 'needs_edit')),
    CONSTRAINT storyboard_shot_transitions_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT storyboard_shot_transitions_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT storyboard_shot_transitions_plan_fk
        FOREIGN KEY (storyboard_plan_id, project_id, production_generation_id)
        REFERENCES storyboard_plans(id, project_id, production_generation_id) ON DELETE CASCADE,
    CONSTRAINT storyboard_shot_transitions_source_shot_fk
        FOREIGN KEY (source_shot_id, project_id, production_generation_id)
        REFERENCES storyboard_shots(id, project_id, production_generation_id) ON DELETE CASCADE,
    CONSTRAINT storyboard_shot_transitions_target_shot_fk
        FOREIGN KEY (target_shot_id, project_id, production_generation_id)
        REFERENCES storyboard_shots(id, project_id, production_generation_id) ON DELETE CASCADE,
    UNIQUE(storyboard_plan_id, target_shot_id, revision)
);

CREATE UNIQUE INDEX storyboard_shot_transitions_one_active_predecessor
    ON storyboard_shot_transitions(storyboard_plan_id, target_shot_id)
    WHERE status = 'active';

CREATE TRIGGER storyboard_shot_transitions_set_updated_at
BEFORE UPDATE ON storyboard_shot_transitions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE shot_reference_packs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    production_generation_id UUID NOT NULL,
    storyboard_shot_id UUID NOT NULL,
    profile_snapshot_hash TEXT NOT NULL,
    shot_state_hash TEXT NOT NULL,
    capability_snapshot_hash TEXT NOT NULL,
    manifest JSONB NOT NULL,
    manifest_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT shot_reference_packs_profile_hash_check CHECK (profile_snapshot_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT shot_reference_packs_state_hash_check CHECK (shot_state_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT shot_reference_packs_capability_hash_check CHECK (capability_snapshot_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT shot_reference_packs_manifest_check CHECK (jsonb_typeof(manifest) = 'object'),
    CONSTRAINT shot_reference_packs_manifest_hash_check CHECK (manifest_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT shot_reference_packs_status_check CHECK (status IN ('active', 'stale', 'archived')),
    CONSTRAINT shot_reference_packs_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT shot_reference_packs_shot_fk
        FOREIGN KEY (storyboard_shot_id, project_id, production_generation_id)
        REFERENCES storyboard_shots(id, project_id, production_generation_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX shot_reference_packs_one_active
    ON shot_reference_packs(storyboard_shot_id)
    WHERE status = 'active';

CREATE TABLE shot_reference_pack_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reference_pack_id UUID NOT NULL REFERENCES shot_reference_packs(id) ON DELETE CASCADE,
    reference_key TEXT NOT NULL,
    role TEXT NOT NULL,
    required BOOLEAN NOT NULL DEFAULT false,
    priority INTEGER NOT NULL DEFAULT 0,
    source_type TEXT NOT NULL,
    source_id UUID,
    asset_id UUID REFERENCES canonical_assets(id) ON DELETE SET NULL,
    artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
    media_file_id UUID REFERENCES media_files(id) ON DELETE SET NULL,
    storage_key TEXT,
    content_hash TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT shot_reference_pack_items_role_check CHECK (role IN ('first_frame', 'last_frame', 'storyboard_sheet', 'character_identity', 'character_costume', 'scene_identity', 'scene_spatial', 'prop_identity', 'continuity_hint', 'motion_reference', 'video_reference', 'audio_reference', 'style_reference')),
    CONSTRAINT shot_reference_pack_items_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT shot_reference_pack_items_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    UNIQUE(reference_pack_id, reference_key)
);

CREATE TABLE shot_visual_anchors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    production_generation_id UUID NOT NULL,
    storyboard_shot_id UUID NOT NULL,
    shot_state_version_id UUID REFERENCES storyboard_shot_state_versions(id) ON DELETE SET NULL,
    anchor_role TEXT NOT NULL,
    revision INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    review_status TEXT NOT NULL DEFAULT 'pending',
    artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
    media_file_id UUID REFERENCES media_files(id) ON DELETE SET NULL,
    storage_key TEXT,
    prompt TEXT,
    prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE SET NULL,
    prompt_hash TEXT,
    provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    model_id UUID REFERENCES provider_models(id) ON DELETE SET NULL,
    reference_pack_id UUID REFERENCES shot_reference_packs(id) ON DELETE SET NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT shot_visual_anchors_role_check CHECK (anchor_role IN ('planned_first_frame', 'planned_last_frame', 'storyboard_sheet', 'storyboard_panel', 'observed_tail_frame', 'continuity_hint')),
    CONSTRAINT shot_visual_anchors_revision_check CHECK (revision > 0),
    CONSTRAINT shot_visual_anchors_status_check CHECK (status IN ('draft', 'generating', 'ready', 'failed', 'stale', 'archived')),
    CONSTRAINT shot_visual_anchors_review_check CHECK (review_status IN ('pending', 'approved', 'rejected', 'needs_edit')),
    CONSTRAINT shot_visual_anchors_prompt_hash_check CHECK (prompt_hash IS NULL OR prompt_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT shot_visual_anchors_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT shot_visual_anchors_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT shot_visual_anchors_shot_fk
        FOREIGN KEY (storyboard_shot_id, project_id, production_generation_id)
        REFERENCES storyboard_shots(id, project_id, production_generation_id) ON DELETE CASCADE,
    UNIQUE(storyboard_shot_id, anchor_role, revision)
);

CREATE UNIQUE INDEX shot_visual_anchors_one_approved
    ON shot_visual_anchors(storyboard_shot_id, anchor_role)
    WHERE status = 'ready' AND review_status = 'approved';

CREATE TRIGGER shot_visual_anchors_set_updated_at
BEFORE UPDATE ON shot_visual_anchors
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO permissions(permission_key, description, name, id, managed_by)
VALUES (
    'project.video_production.rebuild',
    'Rebuild a project under a different immutable video production profile',
    'Project Video Production Rebuild',
    '10000000-0000-4000-a000-000000000001'::uuid,
    'system'
)
ON CONFLICT (permission_key) DO NOTHING;

INSERT INTO role_permissions(role_id, permission_key, managed_by)
SELECT role.id, 'project.video_production.rebuild', 'system'
FROM roles role
WHERE role.role_key IN ('project_owner', 'org_owner', 'organization_owner', 'org_admin', 'organization_admin')
ON CONFLICT DO NOTHING;

-- +goose Down

SET search_path TO public;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM project_video_production_rebuilds)
       OR EXISTS (
           SELECT 1
           FROM projects project
           WHERE (SELECT count(*) FROM project_video_production_generations generation WHERE generation.project_id = project.id) <> 2
       ) THEN
        RAISE EXCEPTION 'cannot roll back video production profiles after new generation business data exists';
    END IF;
END;
$$;
-- +goose StatementEnd

DELETE FROM role_permissions WHERE permission_key = 'project.video_production.rebuild' AND managed_by = 'system';
DELETE FROM permissions WHERE permission_key = 'project.video_production.rebuild' AND managed_by = 'system';

DROP TRIGGER IF EXISTS shot_visual_anchors_set_updated_at ON shot_visual_anchors;
DROP TABLE IF EXISTS shot_visual_anchors;
DROP TABLE IF EXISTS shot_reference_pack_items;
DROP TABLE IF EXISTS shot_reference_packs;
DROP TRIGGER IF EXISTS storyboard_shot_transitions_set_updated_at ON storyboard_shot_transitions;
DROP TABLE IF EXISTS storyboard_shot_transitions;
DROP TABLE IF EXISTS storyboard_shot_state_versions;

DROP INDEX IF EXISTS cost_records_generation_idx;
DROP INDEX IF EXISTS provider_call_logs_generation_idx;
DROP INDEX IF EXISTS provider_async_tasks_generation_idx;
DROP INDEX IF EXISTS video_render_plans_generation_idx;
DROP INDEX IF EXISTS shot_asset_requirements_generation_idx;
DROP INDEX IF EXISTS storyboard_shots_generation_idx;
DROP INDEX IF EXISTS storyboard_plans_generation_idx;
DROP INDEX IF EXISTS workflow_runs_generation_idx;

ALTER TABLE cost_records DROP CONSTRAINT IF EXISTS cost_records_production_generation_fk;
ALTER TABLE cost_records DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE provider_call_logs DROP CONSTRAINT IF EXISTS provider_call_logs_production_generation_fk;
ALTER TABLE provider_call_logs DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE provider_async_tasks DROP CONSTRAINT IF EXISTS provider_async_tasks_video_generation_required;
ALTER TABLE provider_async_tasks DROP CONSTRAINT IF EXISTS provider_async_tasks_production_generation_fk;
ALTER TABLE provider_async_tasks DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE media_files DROP CONSTRAINT IF EXISTS media_files_production_generation_fk;
ALTER TABLE media_files DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE artifacts DROP CONSTRAINT IF EXISTS artifacts_production_generation_fk;
ALTER TABLE artifacts DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE final_video_versions DROP CONSTRAINT IF EXISTS final_video_versions_production_generation_fk;
ALTER TABLE final_video_versions DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE timeline_clips DROP CONSTRAINT IF EXISTS timeline_clips_production_generation_fk;
ALTER TABLE timeline_clips DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE project_timelines DROP CONSTRAINT IF EXISTS project_timelines_production_generation_fk;
ALTER TABLE project_timelines DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE video_render_segments DROP CONSTRAINT IF EXISTS video_render_segments_production_generation_fk;
ALTER TABLE video_render_segments DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE video_render_plans DROP CONSTRAINT IF EXISTS video_render_plans_production_generation_fk;
ALTER TABLE video_render_plans DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE shot_asset_requirements DROP CONSTRAINT IF EXISTS shot_asset_requirements_production_generation_fk;
ALTER TABLE shot_asset_requirements DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE storyboard_shots DROP CONSTRAINT IF EXISTS storyboard_shots_production_generation_fk;
ALTER TABLE storyboard_shots DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE storyboard_plans DROP CONSTRAINT IF EXISTS storyboard_plans_production_generation_fk;
ALTER TABLE storyboard_plans DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE workflow_runs DROP CONSTRAINT IF EXISTS workflow_runs_production_generation_fk;
ALTER TABLE workflow_runs DROP COLUMN IF EXISTS production_generation_id;

ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_active_video_production_generation_fk;
ALTER TABLE projects
    DROP CONSTRAINT IF EXISTS projects_video_production_state_check,
    DROP CONSTRAINT IF EXISTS projects_video_production_generation_no_check,
    DROP COLUMN IF EXISTS video_production_locked,
    DROP COLUMN IF EXISTS video_production_state,
    DROP COLUMN IF EXISTS video_production_generation_no,
    DROP COLUMN IF EXISTS active_video_production_generation_id;

DROP TRIGGER IF EXISTS script_episodes_maintain_revision ON script_episodes;
DROP FUNCTION IF EXISTS maintain_script_episode_revision();
ALTER TABLE script_episodes
    DROP CONSTRAINT IF EXISTS script_episodes_content_hash_check,
    DROP CONSTRAINT IF EXISTS script_episodes_revision_positive,
    DROP COLUMN IF EXISTS content_hash,
    DROP COLUMN IF EXISTS revision;

DROP TABLE IF EXISTS project_video_production_rebuild_attempts;
DROP TABLE IF EXISTS project_video_production_rebuild_items;
ALTER TABLE project_video_production_generations DROP CONSTRAINT IF EXISTS project_video_production_generations_rebuild_fk;
ALTER TABLE project_video_production_bindings DROP CONSTRAINT IF EXISTS project_video_production_bindings_rebuild_fk;
DROP TABLE IF EXISTS project_video_production_rebuilds;
DROP TABLE IF EXISTS project_video_production_generations;
DROP TRIGGER IF EXISTS project_video_production_bindings_append_only ON project_video_production_bindings;
DROP FUNCTION IF EXISTS protect_project_video_production_binding();
DROP TABLE IF EXISTS project_video_production_bindings;
DROP TRIGGER IF EXISTS video_production_profile_versions_immutable ON video_production_profile_versions;
DROP FUNCTION IF EXISTS protect_video_production_profile_version();
DROP TABLE IF EXISTS video_production_profile_versions;
DROP TRIGGER IF EXISTS video_production_profiles_set_updated_at ON video_production_profiles;
DROP TABLE IF EXISTS video_production_profiles;
