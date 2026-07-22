-- +goose Up

SET search_path TO public;

ALTER TABLE workflow_runs
    ADD COLUMN video_production_binding_id UUID,
    ADD COLUMN video_production_binding_revision BIGINT;

UPDATE workflow_runs run
SET video_production_binding_id = generation.binding_id,
    video_production_binding_revision = binding.revision
FROM project_video_production_generations generation
JOIN project_video_production_bindings binding ON binding.id = generation.binding_id
WHERE run.production_generation_id = generation.id;

ALTER TABLE workflow_runs
    ALTER COLUMN video_production_binding_id SET NOT NULL,
    ALTER COLUMN video_production_binding_revision SET NOT NULL,
    ADD CONSTRAINT workflow_runs_video_production_binding_revision_check CHECK (video_production_binding_revision > 0),
    ADD CONSTRAINT workflow_runs_video_production_binding_fk
        FOREIGN KEY (video_production_binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id) ON DELETE RESTRICT;

ALTER TABLE workflow_node_runs ADD COLUMN production_generation_id UUID;
UPDATE workflow_node_runs node
SET production_generation_id = run.production_generation_id
FROM workflow_runs run
WHERE node.workflow_run_id = run.id;
ALTER TABLE workflow_node_runs
    ALTER COLUMN production_generation_id SET NOT NULL,
    ADD CONSTRAINT workflow_node_runs_production_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;

ALTER TABLE workflow_input_snapshots ADD COLUMN production_generation_id UUID;
UPDATE workflow_input_snapshots snapshot
SET production_generation_id = run.production_generation_id
FROM workflow_runs run
WHERE snapshot.workflow_run_id = run.id;
ALTER TABLE workflow_input_snapshots
    ALTER COLUMN production_generation_id SET NOT NULL,
    ADD CONSTRAINT workflow_input_snapshots_production_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;

ALTER TABLE workflow_start_outbox ADD COLUMN production_generation_id UUID;
WITH legacy AS (
    SELECT project_id, id AS generation_id
    FROM project_video_production_generations
    WHERE generation_no = 1 AND status = 'superseded'
), resolved AS (
    SELECT outbox.id, COALESCE(run.production_generation_id, legacy.generation_id) AS generation_id
    FROM workflow_start_outbox outbox
    LEFT JOIN workflow_runs run ON run.id = outbox.workflow_run_id
    LEFT JOIN legacy ON legacy.project_id = outbox.project_id
)
UPDATE workflow_start_outbox outbox
SET production_generation_id = resolved.generation_id
FROM resolved
WHERE outbox.id = resolved.id;
ALTER TABLE workflow_start_outbox
    ALTER COLUMN production_generation_id SET NOT NULL,
    ADD CONSTRAINT workflow_start_outbox_production_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;

ALTER TABLE provider_requests
    ADD COLUMN production_generation_id UUID,
    ADD COLUMN video_production_binding_id UUID,
    ADD COLUMN video_production_binding_revision BIGINT;

WITH legacy AS (
    SELECT project_id, id AS generation_id, binding_id
    FROM project_video_production_generations
    WHERE generation_no = 1 AND status = 'superseded'
), resolved AS (
    SELECT request.id,
           COALESCE(run.production_generation_id, legacy.generation_id) AS generation_id,
           COALESCE(run.video_production_binding_id, legacy.binding_id) AS binding_id
    FROM provider_requests request
    LEFT JOIN workflow_runs run ON run.id = request.workflow_run_id
    LEFT JOIN legacy ON legacy.project_id = request.project_id
    WHERE request.project_id IS NOT NULL
)
UPDATE provider_requests request
SET production_generation_id = resolved.generation_id,
    video_production_binding_id = resolved.binding_id,
    video_production_binding_revision = binding.revision
FROM resolved
JOIN project_video_production_bindings binding ON binding.id = resolved.binding_id
WHERE request.id = resolved.id;

ALTER TABLE provider_requests
    ADD CONSTRAINT provider_requests_production_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT provider_requests_video_production_binding_fk
        FOREIGN KEY (video_production_binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT provider_requests_video_production_binding_revision_check CHECK (
        video_production_binding_revision IS NULL OR video_production_binding_revision > 0
    ),
    ADD CONSTRAINT provider_requests_video_generation_identity_required CHECK (
        project_id IS NULL
        OR task_type NOT LIKE 'video.%'
        OR (
            production_generation_id IS NOT NULL
            AND video_production_binding_id IS NOT NULL
            AND video_production_binding_revision IS NOT NULL
        )
    );

ALTER TABLE provider_async_tasks
    ADD COLUMN video_production_binding_id UUID,
    ADD COLUMN video_production_binding_revision BIGINT;

UPDATE provider_async_tasks task
SET video_production_binding_id = generation.binding_id,
    video_production_binding_revision = binding.revision
FROM project_video_production_generations generation
JOIN project_video_production_bindings binding ON binding.id = generation.binding_id
WHERE task.production_generation_id = generation.id;

ALTER TABLE provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_video_production_binding_fk
        FOREIGN KEY (video_production_binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT provider_async_tasks_video_production_binding_revision_check CHECK (
        video_production_binding_revision IS NULL OR video_production_binding_revision > 0
    ),
    ADD CONSTRAINT provider_async_tasks_video_binding_identity_required CHECK (
        project_id IS NULL
        OR task_type NOT LIKE 'video.%'
        OR (
            video_production_binding_id IS NOT NULL
            AND video_production_binding_revision IS NOT NULL
        )
    );

ALTER TABLE native_audio_reviews ADD COLUMN production_generation_id UUID;
UPDATE native_audio_reviews review
SET production_generation_id = plan.production_generation_id
FROM video_render_plans plan
WHERE review.video_render_plan_id = plan.id;
ALTER TABLE native_audio_reviews
    ALTER COLUMN production_generation_id SET NOT NULL,
    ADD CONSTRAINT native_audio_reviews_production_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;

CREATE INDEX workflow_runs_video_production_identity_idx
    ON workflow_runs(project_id, production_generation_id, video_production_binding_id, video_production_binding_revision);
CREATE INDEX workflow_node_runs_generation_idx
    ON workflow_node_runs(project_id, production_generation_id, status);
CREATE INDEX provider_requests_generation_idx
    ON provider_requests(project_id, production_generation_id, status, updated_at DESC);

CREATE TABLE prompt_context_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    production_generation_id UUID NOT NULL,
    video_production_binding_id UUID NOT NULL,
    video_production_binding_revision BIGINT NOT NULL,
    storyboard_plan_id UUID NOT NULL,
    storyboard_shot_id UUID NOT NULL,
    script_episode_id UUID NOT NULL REFERENCES script_episodes(id) ON DELETE RESTRICT,
    script_scene_id UUID REFERENCES script_scenes(id) ON DELETE SET NULL,
    revision INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    episode_continuity_digest TEXT NOT NULL,
    current_scene_script TEXT NOT NULL,
    adjacent_scene_summaries JSONB NOT NULL DEFAULT '[]'::jsonb,
    current_shot_state JSONB NOT NULL,
    verbatim_dialogue_cues JSONB NOT NULL DEFAULT '[]'::jsonb,
    model_context_limit INTEGER NOT NULL,
    model_prompt_limit INTEGER NOT NULL,
    budget_allocation JSONB NOT NULL,
    source_hashes JSONB NOT NULL,
    plan_hash TEXT NOT NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    stale_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    CONSTRAINT prompt_context_plans_revision_check CHECK (revision > 0),
    CONSTRAINT prompt_context_plans_status_check CHECK (status IN ('active', 'stale', 'archived')),
    CONSTRAINT prompt_context_plans_binding_revision_check CHECK (video_production_binding_revision > 0),
    CONSTRAINT prompt_context_plans_adjacent_check CHECK (jsonb_typeof(adjacent_scene_summaries) = 'array'),
    CONSTRAINT prompt_context_plans_shot_state_check CHECK (jsonb_typeof(current_shot_state) = 'object'),
    CONSTRAINT prompt_context_plans_dialogue_check CHECK (jsonb_typeof(verbatim_dialogue_cues) = 'array'),
    CONSTRAINT prompt_context_plans_context_limit_check CHECK (model_context_limit > 0),
    CONSTRAINT prompt_context_plans_prompt_limit_check CHECK (model_prompt_limit > 0),
    CONSTRAINT prompt_context_plans_budget_check CHECK (jsonb_typeof(budget_allocation) = 'object'),
    CONSTRAINT prompt_context_plans_source_hashes_check CHECK (jsonb_typeof(source_hashes) = 'object'),
    CONSTRAINT prompt_context_plans_hash_check CHECK (plan_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT prompt_context_plans_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT prompt_context_plans_binding_fk
        FOREIGN KEY (video_production_binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT prompt_context_plans_storyboard_plan_fk
        FOREIGN KEY (storyboard_plan_id, project_id, production_generation_id)
        REFERENCES storyboard_plans(id, project_id, production_generation_id) ON DELETE CASCADE,
    CONSTRAINT prompt_context_plans_storyboard_shot_fk
        FOREIGN KEY (storyboard_shot_id, project_id, production_generation_id)
        REFERENCES storyboard_shots(id, project_id, production_generation_id) ON DELETE CASCADE,
    UNIQUE(storyboard_shot_id, revision)
);

CREATE UNIQUE INDEX prompt_context_plans_one_active
    ON prompt_context_plans(storyboard_shot_id)
    WHERE status = 'active';
CREATE INDEX prompt_context_plans_episode_idx
    ON prompt_context_plans(project_id, production_generation_id, script_episode_id, status);

-- +goose StatementBegin
CREATE FUNCTION protect_prompt_context_plan()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status IN ('stale', 'archived') AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'stale or archived prompt context plans are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'active' AND (
        NEW.id IS DISTINCT FROM OLD.id
        OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
        OR NEW.project_id IS DISTINCT FROM OLD.project_id
        OR NEW.production_generation_id IS DISTINCT FROM OLD.production_generation_id
        OR NEW.video_production_binding_id IS DISTINCT FROM OLD.video_production_binding_id
        OR NEW.video_production_binding_revision IS DISTINCT FROM OLD.video_production_binding_revision
        OR NEW.storyboard_plan_id IS DISTINCT FROM OLD.storyboard_plan_id
        OR NEW.storyboard_shot_id IS DISTINCT FROM OLD.storyboard_shot_id
        OR NEW.script_episode_id IS DISTINCT FROM OLD.script_episode_id
        OR NEW.script_scene_id IS DISTINCT FROM OLD.script_scene_id
        OR NEW.revision IS DISTINCT FROM OLD.revision
        OR NEW.episode_continuity_digest IS DISTINCT FROM OLD.episode_continuity_digest
        OR NEW.current_scene_script IS DISTINCT FROM OLD.current_scene_script
        OR NEW.adjacent_scene_summaries IS DISTINCT FROM OLD.adjacent_scene_summaries
        OR NEW.current_shot_state IS DISTINCT FROM OLD.current_shot_state
        OR NEW.verbatim_dialogue_cues IS DISTINCT FROM OLD.verbatim_dialogue_cues
        OR NEW.model_context_limit IS DISTINCT FROM OLD.model_context_limit
        OR NEW.model_prompt_limit IS DISTINCT FROM OLD.model_prompt_limit
        OR NEW.budget_allocation IS DISTINCT FROM OLD.budget_allocation
        OR NEW.source_hashes IS DISTINCT FROM OLD.source_hashes
        OR NEW.plan_hash IS DISTINCT FROM OLD.plan_hash
        OR NEW.created_by IS DISTINCT FROM OLD.created_by
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
        OR NEW.status NOT IN ('active', 'stale', 'archived')
    ) THEN
        RAISE EXCEPTION 'active prompt context plans are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER prompt_context_plans_immutable
BEFORE UPDATE ON prompt_context_plans
FOR EACH ROW EXECUTE FUNCTION protect_prompt_context_plan();

CREATE TABLE video_prompt_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    production_generation_id UUID NOT NULL,
    video_production_binding_id UUID NOT NULL,
    video_production_binding_revision BIGINT NOT NULL,
    profile_version_id UUID NOT NULL REFERENCES video_production_profile_versions(id) ON DELETE RESTRICT,
    storyboard_shot_id UUID NOT NULL,
    prompt_context_plan_id UUID NOT NULL REFERENCES prompt_context_plans(id) ON DELETE RESTRICT,
    prompt_version_id UUID NOT NULL REFERENCES prompt_versions(id) ON DELETE RESTRICT,
    reviewer_prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE SET NULL,
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    node_run_id UUID REFERENCES workflow_node_runs(id) ON DELETE SET NULL,
    provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    reviewer_provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    provider_model_id UUID REFERENCES provider_models(id) ON DELETE SET NULL,
    revision INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    rendered_prompt TEXT NOT NULL,
    prompt_hash TEXT NOT NULL,
    prompt_context_plan_hash TEXT NOT NULL,
    profile_snapshot_hash TEXT NOT NULL,
    shot_state_hash TEXT NOT NULL,
    transition_hash TEXT,
    reference_pack_hash TEXT NOT NULL,
    capability_snapshot_hash TEXT NOT NULL,
    input_contract_version TEXT NOT NULL,
    dialogue_cues JSONB NOT NULL DEFAULT '[]'::jsonb,
    native_audio_required BOOLEAN NOT NULL DEFAULT false,
    audio_strategy TEXT NOT NULL,
    audio_requirement TEXT NOT NULL,
    reviewer_output JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at TIMESTAMPTZ,
    approved_at TIMESTAMPTZ,
    stale_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    CONSTRAINT video_prompt_plans_revision_check CHECK (revision > 0),
    CONSTRAINT video_prompt_plans_binding_revision_check CHECK (video_production_binding_revision > 0),
    CONSTRAINT video_prompt_plans_status_check CHECK (status IN ('draft', 'generating', 'reviewing', 'approved', 'rejected', 'failed', 'stale', 'archived')),
    CONSTRAINT video_prompt_plans_prompt_check CHECK (btrim(rendered_prompt) <> ''),
    CONSTRAINT video_prompt_plans_prompt_hash_check CHECK (prompt_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT video_prompt_plans_context_hash_check CHECK (prompt_context_plan_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT video_prompt_plans_profile_hash_check CHECK (profile_snapshot_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT video_prompt_plans_shot_state_hash_check CHECK (shot_state_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT video_prompt_plans_transition_hash_check CHECK (transition_hash IS NULL OR transition_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT video_prompt_plans_reference_hash_check CHECK (reference_pack_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT video_prompt_plans_capability_hash_check CHECK (capability_snapshot_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT video_prompt_plans_dialogue_check CHECK (jsonb_typeof(dialogue_cues) = 'array'),
    CONSTRAINT video_prompt_plans_audio_strategy_check CHECK (audio_strategy IN ('native_av', 'hybrid', 'tts_postdub')),
    CONSTRAINT video_prompt_plans_audio_requirement_check CHECK (audio_requirement IN ('preferred', 'required', 'disabled')),
    CONSTRAINT video_prompt_plans_reviewer_output_check CHECK (jsonb_typeof(reviewer_output) = 'object'),
    CONSTRAINT video_prompt_plans_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT video_prompt_plans_approval_check CHECK (
        (status = 'approved' AND approved_at IS NOT NULL AND reviewed_at IS NOT NULL)
        OR status <> 'approved'
    ),
    CONSTRAINT video_prompt_plans_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT video_prompt_plans_binding_fk
        FOREIGN KEY (video_production_binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT video_prompt_plans_storyboard_shot_fk
        FOREIGN KEY (storyboard_shot_id, project_id, production_generation_id)
        REFERENCES storyboard_shots(id, project_id, production_generation_id) ON DELETE CASCADE,
    UNIQUE(storyboard_shot_id, revision)
);

CREATE UNIQUE INDEX video_prompt_plans_one_approved
    ON video_prompt_plans(storyboard_shot_id)
    WHERE status = 'approved';
CREATE INDEX video_prompt_plans_generation_status_idx
    ON video_prompt_plans(project_id, production_generation_id, status, created_at DESC);

-- +goose StatementBegin
CREATE FUNCTION protect_reviewed_video_prompt_plan()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status IN ('stale', 'archived') AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'stale or archived video prompt plans are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'approved' AND (
        NEW.id IS DISTINCT FROM OLD.id
        OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
        OR NEW.project_id IS DISTINCT FROM OLD.project_id
        OR NEW.production_generation_id IS DISTINCT FROM OLD.production_generation_id
        OR NEW.video_production_binding_id IS DISTINCT FROM OLD.video_production_binding_id
        OR NEW.video_production_binding_revision IS DISTINCT FROM OLD.video_production_binding_revision
        OR NEW.profile_version_id IS DISTINCT FROM OLD.profile_version_id
        OR NEW.storyboard_shot_id IS DISTINCT FROM OLD.storyboard_shot_id
        OR NEW.prompt_context_plan_id IS DISTINCT FROM OLD.prompt_context_plan_id
        OR NEW.prompt_version_id IS DISTINCT FROM OLD.prompt_version_id
        OR NEW.reviewer_prompt_version_id IS DISTINCT FROM OLD.reviewer_prompt_version_id
        OR NEW.provider_call_id IS DISTINCT FROM OLD.provider_call_id
        OR NEW.reviewer_provider_call_id IS DISTINCT FROM OLD.reviewer_provider_call_id
        OR NEW.provider_model_id IS DISTINCT FROM OLD.provider_model_id
        OR NEW.revision IS DISTINCT FROM OLD.revision
        OR NEW.rendered_prompt IS DISTINCT FROM OLD.rendered_prompt
        OR NEW.prompt_hash IS DISTINCT FROM OLD.prompt_hash
        OR NEW.prompt_context_plan_hash IS DISTINCT FROM OLD.prompt_context_plan_hash
        OR NEW.profile_snapshot_hash IS DISTINCT FROM OLD.profile_snapshot_hash
        OR NEW.shot_state_hash IS DISTINCT FROM OLD.shot_state_hash
        OR NEW.transition_hash IS DISTINCT FROM OLD.transition_hash
        OR NEW.reference_pack_hash IS DISTINCT FROM OLD.reference_pack_hash
        OR NEW.capability_snapshot_hash IS DISTINCT FROM OLD.capability_snapshot_hash
        OR NEW.input_contract_version IS DISTINCT FROM OLD.input_contract_version
        OR NEW.dialogue_cues IS DISTINCT FROM OLD.dialogue_cues
        OR NEW.native_audio_required IS DISTINCT FROM OLD.native_audio_required
        OR NEW.audio_strategy IS DISTINCT FROM OLD.audio_strategy
        OR NEW.audio_requirement IS DISTINCT FROM OLD.audio_requirement
        OR NEW.reviewer_output IS DISTINCT FROM OLD.reviewer_output
        OR NEW.created_by IS DISTINCT FROM OLD.created_by
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
        OR NEW.reviewed_at IS DISTINCT FROM OLD.reviewed_at
        OR NEW.approved_at IS DISTINCT FROM OLD.approved_at
        OR NEW.status NOT IN ('approved', 'stale', 'archived')
    ) THEN
        RAISE EXCEPTION 'approved video prompt plans are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER video_prompt_plans_reviewed_immutable
BEFORE UPDATE ON video_prompt_plans
FOR EACH ROW EXECUTE FUNCTION protect_reviewed_video_prompt_plan();

CREATE TABLE video_prompt_plan_dialogue_cues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_prompt_plan_id UUID NOT NULL REFERENCES video_prompt_plans(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL,
    speaker TEXT NOT NULL,
    dialogue_text TEXT NOT NULL,
    start_tick BIGINT NOT NULL,
    end_tick BIGINT NOT NULL,
    language TEXT NOT NULL DEFAULT 'zh-CN',
    delivery TEXT,
    required BOOLEAN NOT NULL DEFAULT true,
    content_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT video_prompt_plan_dialogue_cues_ordinal_check CHECK (ordinal >= 0),
    CONSTRAINT video_prompt_plan_dialogue_cues_speaker_check CHECK (btrim(speaker) <> ''),
    CONSTRAINT video_prompt_plan_dialogue_cues_text_check CHECK (btrim(dialogue_text) <> ''),
    CONSTRAINT video_prompt_plan_dialogue_cues_ticks_check CHECK (start_tick >= 0 AND end_tick > start_tick),
    CONSTRAINT video_prompt_plan_dialogue_cues_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    UNIQUE(video_prompt_plan_id, ordinal)
);

-- +goose StatementBegin
CREATE FUNCTION protect_approved_video_prompt_dialogue_cue()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    parent_status TEXT;
    parent_id UUID;
BEGIN
    parent_id := COALESCE(NEW.video_prompt_plan_id, OLD.video_prompt_plan_id);
    SELECT status INTO parent_status FROM video_prompt_plans WHERE id = parent_id;
    IF parent_status IN ('approved', 'stale', 'archived') THEN
        RAISE EXCEPTION 'dialogue cues of reviewed video prompt plans are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER video_prompt_plan_dialogue_cues_immutable
BEFORE INSERT OR UPDATE OR DELETE ON video_prompt_plan_dialogue_cues
FOR EACH ROW EXECUTE FUNCTION protect_approved_video_prompt_dialogue_cue();

CREATE TABLE video_native_audio_contracts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    production_generation_id UUID NOT NULL,
    storyboard_shot_id UUID NOT NULL,
    video_prompt_plan_id UUID NOT NULL REFERENCES video_prompt_plans(id) ON DELETE RESTRICT,
    revision INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    audio_strategy TEXT NOT NULL,
    audio_requirement TEXT NOT NULL,
    native_audio_required BOOLEAN NOT NULL,
    dialogue_language TEXT NOT NULL DEFAULT 'zh-CN',
    dialogue_cues_hash TEXT NOT NULL,
    expected_dialogue_duration_ticks BIGINT NOT NULL DEFAULT 0,
    timeline_timebase BIGINT NOT NULL,
    capability_requirements JSONB NOT NULL DEFAULT '{}'::jsonb,
    contract_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    stale_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    CONSTRAINT video_native_audio_contracts_revision_check CHECK (revision > 0),
    CONSTRAINT video_native_audio_contracts_status_check CHECK (status IN ('active', 'stale', 'archived')),
    CONSTRAINT video_native_audio_contracts_strategy_check CHECK (audio_strategy IN ('native_av', 'hybrid', 'tts_postdub')),
    CONSTRAINT video_native_audio_contracts_requirement_check CHECK (audio_requirement IN ('preferred', 'required', 'disabled')),
    CONSTRAINT video_native_audio_contracts_duration_check CHECK (expected_dialogue_duration_ticks >= 0),
    CONSTRAINT video_native_audio_contracts_timebase_check CHECK (timeline_timebase > 0),
    CONSTRAINT video_native_audio_contracts_capability_check CHECK (jsonb_typeof(capability_requirements) = 'object'),
    CONSTRAINT video_native_audio_contracts_dialogue_hash_check CHECK (dialogue_cues_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT video_native_audio_contracts_hash_check CHECK (contract_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT video_native_audio_contracts_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT video_native_audio_contracts_shot_fk
        FOREIGN KEY (storyboard_shot_id, project_id, production_generation_id)
        REFERENCES storyboard_shots(id, project_id, production_generation_id) ON DELETE CASCADE,
    UNIQUE(storyboard_shot_id, revision)
);

CREATE UNIQUE INDEX video_native_audio_contracts_one_active
    ON video_native_audio_contracts(storyboard_shot_id)
    WHERE status = 'active';

ALTER TABLE video_render_plans
    ADD COLUMN video_production_binding_id UUID,
    ADD COLUMN video_production_binding_revision BIGINT,
    ADD COLUMN profile_version_id UUID REFERENCES video_production_profile_versions(id) ON DELETE RESTRICT,
    ADD COLUMN production_profile_snapshot JSONB,
    ADD COLUMN production_profile_snapshot_hash TEXT,
    ADD COLUMN shot_state_revision INTEGER,
    ADD COLUMN shot_state_hash TEXT,
    ADD COLUMN transition_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN transition_hash TEXT,
    ADD COLUMN reference_pack_id UUID REFERENCES shot_reference_packs(id) ON DELETE RESTRICT,
    ADD COLUMN reference_pack_hash TEXT,
    ADD COLUMN input_contract_snapshot JSONB,
    ADD COLUMN input_contract_hash TEXT,
    ADD COLUMN prompt_context_plan_id UUID REFERENCES prompt_context_plans(id) ON DELETE RESTRICT,
    ADD COLUMN prompt_context_plan_hash TEXT,
    ADD COLUMN video_prompt_plan_id UUID REFERENCES video_prompt_plans(id) ON DELETE RESTRICT,
    ADD COLUMN dialogue_cues JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN native_audio_required BOOLEAN NOT NULL DEFAULT false;

UPDATE video_render_plans plan
SET video_production_binding_id = generation.binding_id,
    video_production_binding_revision = binding.revision,
    profile_version_id = binding.profile_version_id,
    production_profile_snapshot = binding.profile_snapshot,
    production_profile_snapshot_hash = binding.profile_snapshot_hash
FROM project_video_production_generations generation
JOIN project_video_production_bindings binding ON binding.id = generation.binding_id
WHERE plan.production_generation_id = generation.id;

ALTER TABLE video_render_plans
    ALTER COLUMN video_production_binding_id SET NOT NULL,
    ALTER COLUMN video_production_binding_revision SET NOT NULL,
    ALTER COLUMN profile_version_id SET NOT NULL,
    ALTER COLUMN production_profile_snapshot SET NOT NULL,
    ALTER COLUMN production_profile_snapshot_hash SET NOT NULL,
    ADD CONSTRAINT video_render_plans_binding_revision_check CHECK (video_production_binding_revision > 0),
    ADD CONSTRAINT video_render_plans_profile_snapshot_check CHECK (jsonb_typeof(production_profile_snapshot) = 'object'),
    ADD CONSTRAINT video_render_plans_profile_snapshot_hash_check CHECK (production_profile_snapshot_hash ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT video_render_plans_shot_state_revision_check CHECK (shot_state_revision IS NULL OR shot_state_revision > 0),
    ADD CONSTRAINT video_render_plans_shot_state_hash_check CHECK (shot_state_hash IS NULL OR shot_state_hash ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT video_render_plans_transition_snapshot_check CHECK (jsonb_typeof(transition_snapshot) = 'object'),
    ADD CONSTRAINT video_render_plans_transition_hash_check CHECK (transition_hash IS NULL OR transition_hash ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT video_render_plans_reference_pack_hash_check CHECK (reference_pack_hash IS NULL OR reference_pack_hash ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT video_render_plans_input_contract_snapshot_check CHECK (input_contract_snapshot IS NULL OR jsonb_typeof(input_contract_snapshot) = 'object'),
    ADD CONSTRAINT video_render_plans_input_contract_hash_check CHECK (input_contract_hash IS NULL OR input_contract_hash ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT video_render_plans_prompt_context_hash_check CHECK (prompt_context_plan_hash IS NULL OR prompt_context_plan_hash ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT video_render_plans_dialogue_cues_check CHECK (jsonb_typeof(dialogue_cues) = 'array'),
    ADD CONSTRAINT video_render_plans_binding_fk
        FOREIGN KEY (video_production_binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT video_render_plans_compiled_contract_required CHECK (
        status IN ('archived', 'stale', 'failed', 'cancelled')
        OR (
            shot_state_revision IS NOT NULL
            AND shot_state_hash IS NOT NULL
            AND reference_pack_id IS NOT NULL
            AND reference_pack_hash IS NOT NULL
            AND input_contract_snapshot IS NOT NULL
            AND input_contract_hash IS NOT NULL
            AND prompt_context_plan_id IS NOT NULL
            AND prompt_context_plan_hash IS NOT NULL
            AND video_prompt_plan_id IS NOT NULL
        )
    );

CREATE INDEX video_render_plans_contract_idx
    ON video_render_plans(project_id, production_generation_id, video_prompt_plan_id, status);

CREATE TEMP TABLE video_profile_prompt_seed (
    profile_key TEXT NOT NULL,
    role_key TEXT NOT NULL,
    template_key TEXT NOT NULL,
    name TEXT NOT NULL,
    purpose TEXT NOT NULL,
    content TEXT NOT NULL
) ON COMMIT DROP;

-- +goose StatementBegin
INSERT INTO video_profile_prompt_seed(profile_key, role_key, template_key, name, purpose, content)
VALUES
('single_frame_i2v', 'anchor.plan', 'video_profile.single_frame_i2v.anchor.plan', '图生视频首帧规划', '为单首帧图生视频规划当前镜头的结构化视觉锚点', $prompt$
你是图生视频首帧规划器。只规划当前镜头自己的干净起始画面，不继承前一镜头像素。
必须依据 ShotState、Transition reset/carry、当前镜头 required assets 和项目手册输出严格 JSON。
首帧必须包含当前镜头要求的全部人物、场景与道具，明确机位、景别、站位、视线和动作起点。
不得把台词、字幕、文字、分镜说明或前序视频尾帧写成画面内容。
输入：{{ input.context }}
$prompt$),
('single_frame_i2v', 'anchor.generate', 'video_profile.single_frame_i2v.anchor.generate', '图生视频首帧生成', '生成可直接作为单参考图视频模型起始帧的图片提示词', $prompt$
根据当前镜头已校验的视觉锚点计划生成一段图片提示词。
提示词只描述单张可见画面：人物身份与服装、场景、道具、机位、构图、站位、表情、光线和动作起点。
禁止出现台词、引号、字幕、文字、对白含义、未来动作序列或视频运镜过程。
输出只包含最终图片提示词。
输入：{{ input.context }}
$prompt$),
('single_frame_i2v', 'anchor.review', 'video_profile.single_frame_i2v.anchor.review', '图生视频首帧审核', '审核单首帧是否完整表达当前镜头事实', $prompt$
审核当前首帧与 ShotState、required assets、机位和动作起点是否一致。
逐项检查缺失人物、意外人物、场景、服装道具、站位视线、构图、风格和文字泄漏。
任何 required asset 缺失、人物串位、错误场景或可见文字都必须拒绝。输出严格 JSON。
输入：{{ input.context }}
$prompt$),
('single_frame_i2v', 'video.generate', 'video_profile.single_frame_i2v.video.generate', '图生视频提示词生成', '从唯一已审核首帧生成忠实执行剧本的视频提示词', $prompt$
你正在为单首帧图生视频模型生成视频提示词。首帧是唯一硬视觉输入，动作必须从首帧状态可达。
依据 PromptContextPlan、当前镜头动作、运镜、时长和模型能力生成，不得要求切换到另一个镜头或变形成新人物/场景。
verbatimDialogueCues 中的中文台词必须逐字保留，不得翻译、缩写、润色或遗漏；没有台词时不得编造。
nativeAudioRequired=true 时明确要求角色按说话人和顺序说出原中文台词，并保持环境音与画面同步。
输出只包含最终视频提示词。
输入：{{ input.context }}
$prompt$),
('single_frame_i2v', 'video.review', 'video_profile.single_frame_i2v.video.review', '图生视频提示词审核', '审核单首帧视频提示词的剧本忠实性和可执行性', $prompt$
审核视频提示词是否从当前首帧可达，是否忠实执行当前镜头动作、运镜、时长和逐字中文台词。
拒绝新增人物/场景、跨镜头变形、遗漏或改写 dialogue cue、超过模型限制、错误比例或错误原生音频要求。
Reviewer 必须使用与生成阶段相同的 PromptContextPlan hash，输出严格 JSON。
输入：{{ input.context }}
$prompt$),
('first_last_frame', 'anchor.plan', 'video_profile.first_last_frame.anchor.plan', '首尾帧锚点规划', '规划同一镜头的可达首帧和尾帧', $prompt$
为同一个 StoryboardShot 规划计划首帧和计划尾帧。两帧必须保持人物、服装、场景和道具身份一致，尾帧只能表达本镜头动作完成后的可达状态。
不得把下一镜头状态塞入尾帧，不得跨镜头继承像素。输出严格 JSON。
输入：{{ input.context }}
$prompt$),
('first_last_frame', 'anchor.generate', 'video_profile.first_last_frame.anchor.generate', '首尾帧锚点生成', '分别生成同镜头首帧和尾帧图片提示词', $prompt$
根据已校验状态分别输出 planned_first_frame 与 planned_last_frame 图片提示词。两者都禁止台词、字幕和可见文字，必须保持身份一致并保证动作与位移可达。
输入：{{ input.context }}
$prompt$),
('first_last_frame', 'anchor.review', 'video_profile.first_last_frame.anchor.review', '首尾帧锚点审核', '审核同镜头首尾帧一致性和运动可达性', $prompt$
审核首尾帧人物身份、服装、场景、道具、空间轴和构图是否一致，动作与位移是否能在镜头时长内完成。任一硬约束失败则拒绝，输出严格 JSON。
输入：{{ input.context }}
$prompt$),
('first_last_frame', 'video.generate', 'video_profile.first_last_frame.video.generate', '首尾帧视频提示词生成', '在同镜头首尾帧之间生成视频提示词', $prompt$
生成在已审核首帧与尾帧之间完成动作的视频提示词，不得越过尾帧状态或引入新人物场景。逐字保留结构化中文 dialogue cues，并遵守原生音频契约。
输入：{{ input.context }}
$prompt$),
('first_last_frame', 'video.review', 'video_profile.first_last_frame.video.review', '首尾帧视频提示词审核', '审核首尾帧插值任务的可执行性', $prompt$
审核视频提示词是否保持首尾帧身份一致、动作可达、时长合法，且逐字保留中文 dialogue cues。输出严格 JSON。
输入：{{ input.context }}
$prompt$),
('multimodal_reference', 'anchor.plan', 'video_profile.multimodal_reference.anchor.plan', '多模态锚点规划', '规划主构图锚点和带类型语义参考', $prompt$
规划当前镜头主构图锚点及角色、服装、场景、道具、动作、视频和音频参考角色。每个引用必须有唯一语义，禁止把历史版本或前序尾帧当作当前事实源。输出严格 JSON。
输入：{{ input.context }}
$prompt$),
('multimodal_reference', 'anchor.generate', 'video_profile.multimodal_reference.anchor.generate', '多模态锚点生成', '生成主构图图片提示词并保持引用角色清晰', $prompt$
生成当前镜头主构图锚点的图片提示词。依据 typed ReferencePack 保持角色与场景身份，不得在图片中写台词、字幕或说明文字。
输入：{{ input.context }}
$prompt$),
('multimodal_reference', 'anchor.review', 'video_profile.multimodal_reference.anchor.review', '多模态锚点审核', '审核多模态引用是否完整且未串位', $prompt$
审核主锚点和 typed ReferencePack：required 引用不得缺失，角色不得串位，引用数量与模型限制必须合法，历史或 stale 引用不得进入 active pack。输出严格 JSON。
输入：{{ input.context }}
$prompt$),
('multimodal_reference', 'video.generate', 'video_profile.multimodal_reference.video.generate', '多模态视频提示词生成', '按多类型参考和当前镜头剧本生成视频提示词', $prompt$
依据主锚点、typed semantic references、PromptContextPlan 和模型 Input Contract 生成当前镜头视频提示词。明确区分首帧与语义参考，不得把参考图都描述成输出首帧。逐字保留中文 dialogue cues。
输入：{{ input.context }}
$prompt$),
('multimodal_reference', 'video.review', 'video_profile.multimodal_reference.video.review', '多模态视频提示词审核', '审核多模态引用语义和剧本忠实性', $prompt$
审核引用角色、数量、互斥规则、剧本动作、逐字中文台词和原生音频能力。任何引用串位或适配器无法表达的 contract 都必须拒绝。输出严格 JSON。
输入：{{ input.context }}
$prompt$),
('storyboard_sheet', 'anchor.plan', 'video_profile.storyboard_sheet.anchor.plan', '分镜板锚点规划', '规划同一镜头多个时间点的 PanelManifest', $prompt$
为同一个 StoryboardShot 规划按时间排序的关键帧 PanelManifest。每个 panel 必须属于同一镜头，表达动作阶段与机位演进，不得混入下一镜头。输出严格 JSON。
输入：{{ input.context }}
$prompt$),
('storyboard_sheet', 'anchor.generate', 'video_profile.storyboard_sheet.anchor.generate', '分镜板锚点生成', '生成无文字的单张分镜板图片提示词', $prompt$
生成一张包含有序关键帧 panel 的分镜板图片提示词。所有 panel 必须无文字、无编号、无字幕、无对话框；人物、场景和动作阶段与 PanelManifest 一致。
输入：{{ input.context }}
$prompt$),
('storyboard_sheet', 'anchor.review', 'video_profile.storyboard_sheet.anchor.review', '分镜板锚点审核', '审核 PanelManifest 顺序和无文字约束', $prompt$
审核分镜板 panel 数量、顺序、动作阶段、机位变化、身份一致性和文字泄漏。分镜板不得被误判为输出首帧。输出严格 JSON。
输入：{{ input.context }}
$prompt$),
('storyboard_sheet', 'video.generate', 'video_profile.storyboard_sheet.video.generate', '分镜板视频提示词生成', '按分镜板和 PanelManifest 生成视频提示词', $prompt$
依据已审核分镜板、PanelManifest、当前镜头时长和 PromptContextPlan 生成视频提示词。按 panel 时间顺序完成动作，不得跨入下一镜头，逐字保留中文 dialogue cues。
输入：{{ input.context }}
$prompt$),
('storyboard_sheet', 'video.review', 'video_profile.storyboard_sheet.video.review', '分镜板视频提示词审核', '审核分镜板时序与视频提示词一致性', $prompt$
审核视频提示词是否遵循 PanelManifest 顺序、镜头时长、动作阶段、机位和逐字中文台词，且模型明确支持 storyboard_sheet_reference。输出严格 JSON。
输入：{{ input.context }}
$prompt$);
-- +goose StatementEnd

INSERT INTO prompt_templates(
    id, organization_id, template_key, name, purpose, description, modality,
    task_type, scope, status, is_system, managed_by
)
SELECT
    md5('cineweave:video-profile-template:' || template_key)::uuid,
    NULL,
    template_key,
    name,
    purpose,
    'VideoProductionProfile ' || profile_key || ' contract role ' || role_key,
    'text',
    'text.generate',
    'system',
    'active',
    true,
    'system'
FROM video_profile_prompt_seed;

INSERT INTO prompt_versions(
    id, prompt_template_id, version_no, content, variables_schema, content_hash,
    template_id, version, status, title, content_format, metadata, activated_at, managed_by
)
SELECT
    md5('cineweave:video-profile-prompt-version:' || seed.template_key || ':1')::uuid,
    template.id,
    1,
    seed.content,
    '{"type":"object","required":["input"],"properties":{"input":{"type":"object"}}}'::jsonb,
    'sha256:' || encode(public.digest(pg_catalog.convert_to(seed.content, 'UTF8'), 'sha256'), 'hex'),
    template.id,
    1,
    'active',
    seed.name,
    'text',
    jsonb_build_object(
        'contractVersion', 'video-production-prompt/v1',
        'profileKey', seed.profile_key,
        'role', seed.role_key,
        'seedMigration', '000010_video_production_prompt_contracts'
    ),
    now(),
    'system'
FROM video_profile_prompt_seed seed
JOIN prompt_templates template
  ON template.organization_id IS NULL AND template.template_key = seed.template_key;

-- +goose Down

SET search_path TO public;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM prompt_context_plans)
       OR EXISTS (SELECT 1 FROM video_prompt_plans)
       OR EXISTS (SELECT 1 FROM video_native_audio_contracts) THEN
        RAISE EXCEPTION 'cannot roll back video production prompt contracts after business data exists';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP INDEX IF EXISTS video_render_plans_contract_idx;
ALTER TABLE video_render_plans
    DROP CONSTRAINT IF EXISTS video_render_plans_compiled_contract_required,
    DROP CONSTRAINT IF EXISTS video_render_plans_binding_fk,
    DROP CONSTRAINT IF EXISTS video_render_plans_dialogue_cues_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_prompt_context_hash_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_input_contract_hash_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_input_contract_snapshot_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_reference_pack_hash_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_transition_hash_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_transition_snapshot_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_shot_state_hash_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_shot_state_revision_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_profile_snapshot_hash_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_profile_snapshot_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_binding_revision_check,
    DROP COLUMN IF EXISTS native_audio_required,
    DROP COLUMN IF EXISTS dialogue_cues,
    DROP COLUMN IF EXISTS video_prompt_plan_id,
    DROP COLUMN IF EXISTS prompt_context_plan_hash,
    DROP COLUMN IF EXISTS prompt_context_plan_id,
    DROP COLUMN IF EXISTS input_contract_hash,
    DROP COLUMN IF EXISTS input_contract_snapshot,
    DROP COLUMN IF EXISTS reference_pack_hash,
    DROP COLUMN IF EXISTS reference_pack_id,
    DROP COLUMN IF EXISTS transition_hash,
    DROP COLUMN IF EXISTS transition_snapshot,
    DROP COLUMN IF EXISTS shot_state_hash,
    DROP COLUMN IF EXISTS shot_state_revision,
    DROP COLUMN IF EXISTS production_profile_snapshot_hash,
    DROP COLUMN IF EXISTS production_profile_snapshot,
    DROP COLUMN IF EXISTS profile_version_id,
    DROP COLUMN IF EXISTS video_production_binding_revision,
    DROP COLUMN IF EXISTS video_production_binding_id;

DROP INDEX IF EXISTS video_native_audio_contracts_one_active;
DROP TABLE IF EXISTS video_native_audio_contracts;
DROP TRIGGER IF EXISTS video_prompt_plan_dialogue_cues_immutable ON video_prompt_plan_dialogue_cues;
DROP FUNCTION IF EXISTS protect_approved_video_prompt_dialogue_cue();
DROP TABLE IF EXISTS video_prompt_plan_dialogue_cues;
DROP TRIGGER IF EXISTS video_prompt_plans_reviewed_immutable ON video_prompt_plans;
DROP FUNCTION IF EXISTS protect_reviewed_video_prompt_plan();
DROP INDEX IF EXISTS video_prompt_plans_generation_status_idx;
DROP INDEX IF EXISTS video_prompt_plans_one_approved;
DROP TABLE IF EXISTS video_prompt_plans;
DROP TRIGGER IF EXISTS prompt_context_plans_immutable ON prompt_context_plans;
DROP FUNCTION IF EXISTS protect_prompt_context_plan();
DROP INDEX IF EXISTS prompt_context_plans_episode_idx;
DROP INDEX IF EXISTS prompt_context_plans_one_active;
DROP TABLE IF EXISTS prompt_context_plans;

DELETE FROM prompt_versions version
USING prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key LIKE 'video_profile.%'
  AND version.managed_by = 'system'
  AND version.metadata->>'seedMigration' = '000010_video_production_prompt_contracts';

DELETE FROM prompt_templates
WHERE organization_id IS NULL
  AND template_key LIKE 'video_profile.%'
  AND managed_by = 'system'
  AND description LIKE 'VideoProductionProfile % contract role %';

DROP INDEX IF EXISTS provider_requests_generation_idx;
DROP INDEX IF EXISTS workflow_node_runs_generation_idx;
DROP INDEX IF EXISTS workflow_runs_video_production_identity_idx;

ALTER TABLE native_audio_reviews DROP CONSTRAINT IF EXISTS native_audio_reviews_production_generation_fk;
ALTER TABLE native_audio_reviews DROP COLUMN IF EXISTS production_generation_id;

ALTER TABLE provider_async_tasks
    DROP CONSTRAINT IF EXISTS provider_async_tasks_video_binding_identity_required,
    DROP CONSTRAINT IF EXISTS provider_async_tasks_video_production_binding_revision_check,
    DROP CONSTRAINT IF EXISTS provider_async_tasks_video_production_binding_fk,
    DROP COLUMN IF EXISTS video_production_binding_revision,
    DROP COLUMN IF EXISTS video_production_binding_id;

ALTER TABLE provider_requests
    DROP CONSTRAINT IF EXISTS provider_requests_video_generation_identity_required,
    DROP CONSTRAINT IF EXISTS provider_requests_video_production_binding_revision_check,
    DROP CONSTRAINT IF EXISTS provider_requests_video_production_binding_fk,
    DROP CONSTRAINT IF EXISTS provider_requests_production_generation_fk,
    DROP COLUMN IF EXISTS video_production_binding_revision,
    DROP COLUMN IF EXISTS video_production_binding_id,
    DROP COLUMN IF EXISTS production_generation_id;

ALTER TABLE workflow_start_outbox DROP CONSTRAINT IF EXISTS workflow_start_outbox_production_generation_fk;
ALTER TABLE workflow_start_outbox DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE workflow_input_snapshots DROP CONSTRAINT IF EXISTS workflow_input_snapshots_production_generation_fk;
ALTER TABLE workflow_input_snapshots DROP COLUMN IF EXISTS production_generation_id;
ALTER TABLE workflow_node_runs DROP CONSTRAINT IF EXISTS workflow_node_runs_production_generation_fk;
ALTER TABLE workflow_node_runs DROP COLUMN IF EXISTS production_generation_id;

ALTER TABLE workflow_runs
    DROP CONSTRAINT IF EXISTS workflow_runs_video_production_binding_fk,
    DROP CONSTRAINT IF EXISTS workflow_runs_video_production_binding_revision_check,
    DROP COLUMN IF EXISTS video_production_binding_revision,
    DROP COLUMN IF EXISTS video_production_binding_id;
