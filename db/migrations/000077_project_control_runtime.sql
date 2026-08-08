-- +goose Up
SET search_path TO public;

ALTER TABLE project_sources
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT project_sources_revision_positive CHECK (revision > 0);

-- +goose StatementBegin
CREATE FUNCTION maintain_project_source_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF ROW(
        NEW.source_type,
        NEW.title,
        NEW.content,
        NEW.content_format,
        NEW.original_file_name,
        NEW.storage_key,
        NEW.status,
        NEW.metadata
    ) IS DISTINCT FROM ROW(
        OLD.source_type,
        OLD.title,
        OLD.content,
        OLD.content_format,
        OLD.original_file_name,
        OLD.storage_key,
        OLD.status,
        OLD.metadata
    ) THEN
        NEW.revision := OLD.revision + 1;
    ELSE
        NEW.revision := OLD.revision;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_sources_maintain_revision
BEFORE UPDATE ON project_sources
FOR EACH ROW EXECUTE FUNCTION maintain_project_source_revision();

ALTER TABLE script_scenes
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT script_scenes_revision_positive CHECK (revision > 0);

-- +goose StatementBegin
CREATE FUNCTION maintain_script_scene_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF ROW(
        NEW.script_id,
        NEW.script_version_id,
        NEW.script_episode_id,
        NEW.scene_index,
        NEW.scene_no,
        NEW.title,
        NEW.summary,
        NEW.location,
        NEW.time_of_day,
        NEW.atmosphere,
        NEW.characters,
        NEW.scenes,
        NEW.props,
        NEW.action,
        NEW.dialogue,
        NEW.visual_goal,
        NEW.emotional_tone,
        NEW.conflict,
        NEW.outcome,
        NEW.source_event_ids,
        NEW.content,
        NEW.content_format,
        NEW.review_status,
        NEW.manual_override,
        NEW.stale_state,
        NEW.metadata,
        NEW.edited_by,
        NEW.edited_at,
        NEW.deleted_at
    ) IS DISTINCT FROM ROW(
        OLD.script_id,
        OLD.script_version_id,
        OLD.script_episode_id,
        OLD.scene_index,
        OLD.scene_no,
        OLD.title,
        OLD.summary,
        OLD.location,
        OLD.time_of_day,
        OLD.atmosphere,
        OLD.characters,
        OLD.scenes,
        OLD.props,
        OLD.action,
        OLD.dialogue,
        OLD.visual_goal,
        OLD.emotional_tone,
        OLD.conflict,
        OLD.outcome,
        OLD.source_event_ids,
        OLD.content,
        OLD.content_format,
        OLD.review_status,
        OLD.manual_override,
        OLD.stale_state,
        OLD.metadata,
        OLD.edited_by,
        OLD.edited_at,
        OLD.deleted_at
    ) THEN
        NEW.revision := OLD.revision + 1;
    ELSE
        NEW.revision := OLD.revision;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER script_scenes_maintain_revision
BEFORE UPDATE ON script_scenes
FOR EACH ROW EXECUTE FUNCTION maintain_script_scene_revision();

ALTER TABLE novel_events
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT novel_events_revision_positive CHECK (revision > 0);

-- +goose StatementBegin
CREATE FUNCTION maintain_novel_event_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.revision := OLD.revision + 1;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER novel_events_maintain_revision
BEFORE UPDATE ON novel_events
FOR EACH ROW EXECUTE FUNCTION maintain_novel_event_revision();

ALTER TABLE adaptation_plans
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT adaptation_plans_revision_positive CHECK (revision > 0);

-- +goose StatementBegin
CREATE FUNCTION maintain_adaptation_plan_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.revision := OLD.revision + 1;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER adaptation_plans_maintain_revision
BEFORE UPDATE ON adaptation_plans
FOR EACH ROW EXECUTE FUNCTION maintain_adaptation_plan_revision();

ALTER TABLE character_voice_profiles
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT character_voice_profiles_revision_positive CHECK (revision > 0);

-- +goose StatementBegin
CREATE FUNCTION maintain_character_voice_profile_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.revision := OLD.revision + 1;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER character_voice_profiles_maintain_revision
BEFORE UPDATE ON character_voice_profiles
FOR EACH ROW EXECUTE FUNCTION maintain_character_voice_profile_revision();

ALTER TABLE final_video_versions
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT final_video_versions_revision_positive CHECK (revision > 0);

-- +goose StatementBegin
CREATE FUNCTION maintain_final_video_version_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.revision := OLD.revision + 1;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER final_video_versions_maintain_revision
BEFORE UPDATE ON final_video_versions
FOR EACH ROW EXECUTE FUNCTION maintain_final_video_version_revision();

-- +goose StatementBegin
CREATE FUNCTION maintain_project_timeline_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF ROW(
        NEW.workflow_run_id,
        NEW.title,
        NEW.status,
        NEW.aspect_ratio,
        NEW.resolution,
        NEW.metadata,
        NEW.edited_by,
        NEW.edited_at,
        NEW.manual_override,
        NEW.stale_state,
        NEW.timeline_timebase,
        NEW.fps_numerator,
        NEW.fps_denominator
    ) IS DISTINCT FROM ROW(
        OLD.workflow_run_id,
        OLD.title,
        OLD.status,
        OLD.aspect_ratio,
        OLD.resolution,
        OLD.metadata,
        OLD.edited_by,
        OLD.edited_at,
        OLD.manual_override,
        OLD.stale_state,
        OLD.timeline_timebase,
        OLD.fps_numerator,
        OLD.fps_denominator
    ) THEN
        NEW.revision := OLD.revision + 1;
    ELSE
        NEW.revision := OLD.revision;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_timelines_maintain_revision
BEFORE UPDATE ON project_timelines
FOR EACH ROW EXECUTE FUNCTION maintain_project_timeline_revision();

ALTER TABLE timeline_clips
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT timeline_clips_revision_positive CHECK (revision > 0);

-- +goose StatementBegin
CREATE FUNCTION maintain_timeline_clip_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF ROW(
        NEW.storyboard_shot_id,
        NEW.video_artifact_id,
        NEW.video_media_file_id,
        NEW.clip_index,
        NEW.title,
        NEW.enabled,
        NEW.source_storage_key,
        NEW.notes,
        NEW.metadata,
        NEW.manual_override,
        NEW.stale_state,
        NEW.edited_by,
        NEW.edited_at,
        NEW.start_tick,
        NEW.end_tick,
        NEW.source_duration_ticks,
        NEW.trim_start_tick,
        NEW.trim_end_tick
    ) IS DISTINCT FROM ROW(
        OLD.storyboard_shot_id,
        OLD.video_artifact_id,
        OLD.video_media_file_id,
        OLD.clip_index,
        OLD.title,
        OLD.enabled,
        OLD.source_storage_key,
        OLD.notes,
        OLD.metadata,
        OLD.manual_override,
        OLD.stale_state,
        OLD.edited_by,
        OLD.edited_at,
        OLD.start_tick,
        OLD.end_tick,
        OLD.source_duration_ticks,
        OLD.trim_start_tick,
        OLD.trim_end_tick
    ) THEN
        NEW.revision := OLD.revision + 1;
    ELSE
        NEW.revision := OLD.revision;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER timeline_clips_maintain_revision
BEFORE UPDATE ON timeline_clips
FOR EACH ROW EXECUTE FUNCTION maintain_timeline_clip_revision();

CREATE TABLE user_control_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT 'Codex 项目控制',
    public_id TEXT NOT NULL UNIQUE,
    prefix TEXT NOT NULL,
    secret_hash TEXT NOT NULL UNIQUE,
    credential_version INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    rotated_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT user_control_keys_name_nonempty CHECK (btrim(name) <> ''),
    CONSTRAINT user_control_keys_public_id_nonempty CHECK (btrim(public_id) <> ''),
    CONSTRAINT user_control_keys_prefix_nonempty CHECK (btrim(prefix) <> ''),
    CONSTRAINT user_control_keys_secret_hash_sha256 CHECK (secret_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT user_control_keys_credential_version_nonnegative CHECK (credential_version >= 0),
    CONSTRAINT user_control_keys_status_check CHECK (status IN ('active', 'revoked')),
    CONSTRAINT user_control_keys_revocation_consistent CHECK (
        (status = 'active' AND revoked_at IS NULL)
        OR (status = 'revoked' AND revoked_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX user_control_keys_one_active_per_user_idx
    ON user_control_keys(user_id)
    WHERE status = 'active';

CREATE INDEX user_control_keys_user_created_idx
    ON user_control_keys(user_id, created_at DESC);

CREATE TABLE project_control_commands (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    controller_type TEXT NOT NULL,
    control_key_id UUID REFERENCES user_control_keys(id) ON DELETE SET NULL,
    agent_task_id UUID REFERENCES agent_tasks(id) ON DELETE SET NULL,
    agent_step_id UUID REFERENCES agent_steps(id) ON DELETE SET NULL,
    action_name TEXT NOT NULL,
    action_version INTEGER NOT NULL DEFAULT 1,
    execution_mode TEXT NOT NULL,
    activity_visibility TEXT NOT NULL,
    input JSONB NOT NULL DEFAULT '{}'::jsonb,
    input_hash TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    output JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code TEXT,
    error_message TEXT,
    parent_command_id UUID REFERENCES project_control_commands(id) ON DELETE SET NULL,
    retry_of_command_id UUID REFERENCES project_control_commands(id) ON DELETE SET NULL,
    cancellation_requested_at TIMESTAMPTZ,
    cancellation_requested_by_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    cancellation_idempotency_key TEXT,
    cancellation_reason TEXT,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    next_reconcile_at TIMESTAMPTZ,
    worker_release_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    revision BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT project_control_commands_controller_type_check CHECK (
        controller_type IN ('embedded_agent', 'codex_mcp', 'manual')
    ),
    CONSTRAINT project_control_commands_controller_identity_check CHECK (
        (controller_type = 'embedded_agent' AND control_key_id IS NULL AND agent_task_id IS NOT NULL AND agent_step_id IS NOT NULL)
        OR (controller_type = 'codex_mcp' AND control_key_id IS NOT NULL AND agent_task_id IS NULL AND agent_step_id IS NULL)
        OR (controller_type = 'manual' AND control_key_id IS NULL AND agent_task_id IS NULL AND agent_step_id IS NULL)
    ),
    CONSTRAINT project_control_commands_scope_check CHECK (
        project_id IS NULL OR workspace_id IS NOT NULL
    ),
    CONSTRAINT project_control_commands_action_name_nonempty CHECK (btrim(action_name) <> ''),
    CONSTRAINT project_control_commands_action_version_positive CHECK (action_version > 0),
    CONSTRAINT project_control_commands_execution_mode_check CHECK (
        execution_mode IN ('sync', 'async_command', 'workflow')
    ),
    CONSTRAINT project_control_commands_activity_visibility_check CHECK (
        activity_visibility IN ('primary', 'nested', 'audit_only')
    ),
    CONSTRAINT project_control_commands_input_object_check CHECK (jsonb_typeof(input) = 'object'),
    CONSTRAINT project_control_commands_input_size_check CHECK (octet_length(input::text) <= 65536),
    CONSTRAINT project_control_commands_input_hash_sha256 CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT project_control_commands_idempotency_key_size_check CHECK (
        char_length(idempotency_key) BETWEEN 1 AND 200
    ),
    CONSTRAINT project_control_commands_status_check CHECK (
        status IN (
            'queued', 'running', 'waiting_workflow', 'waiting_input',
            'succeeded', 'partial_succeeded', 'failed', 'cancelled'
        )
    ),
    CONSTRAINT project_control_commands_output_object_check CHECK (jsonb_typeof(output) = 'object'),
    CONSTRAINT project_control_commands_output_size_check CHECK (octet_length(output::text) <= 65536),
    CONSTRAINT project_control_commands_failure_error_check CHECK (
        status <> 'failed' OR (error_code IS NOT NULL AND btrim(error_code) <> '')
    ),
    CONSTRAINT project_control_commands_terminal_time_check CHECK (
        (status IN ('succeeded', 'partial_succeeded', 'failed', 'cancelled')) = (completed_at IS NOT NULL)
    ),
    CONSTRAINT project_control_commands_lease_pair_check CHECK (
        (lease_owner IS NULL) = (lease_expires_at IS NULL)
    ),
    CONSTRAINT project_control_commands_revision_positive CHECK (revision > 0),
    CONSTRAINT project_control_commands_retry_not_self CHECK (retry_of_command_id IS NULL OR retry_of_command_id <> id),
    CONSTRAINT project_control_commands_parent_not_self CHECK (parent_command_id IS NULL OR parent_command_id <> id)
    ,CONSTRAINT project_control_commands_cancellation_request_check CHECK (
        (cancellation_requested_at IS NULL
            AND cancellation_requested_by_user_id IS NULL
            AND cancellation_idempotency_key IS NULL
            AND cancellation_reason IS NULL)
        OR (cancellation_requested_at IS NOT NULL
            AND cancellation_requested_by_user_id IS NOT NULL
            AND cancellation_idempotency_key IS NOT NULL
            AND char_length(cancellation_idempotency_key) BETWEEN 1 AND 200)
    )
);

CREATE UNIQUE INDEX project_control_commands_idempotency_idx
    ON project_control_commands(actor_user_id, controller_type, idempotency_key);

CREATE UNIQUE INDEX project_control_commands_one_active_retry_idx
    ON project_control_commands(retry_of_command_id)
    WHERE retry_of_command_id IS NOT NULL
      AND status IN ('queued', 'running', 'waiting_workflow', 'waiting_input');

CREATE INDEX project_control_commands_project_activity_idx
    ON project_control_commands(project_id, status, updated_at DESC)
    WHERE project_id IS NOT NULL;

CREATE INDEX project_control_commands_actor_recent_idx
    ON project_control_commands(actor_user_id, created_at DESC);

CREATE INDEX project_control_commands_dispatch_idx
    ON project_control_commands(status, lease_expires_at, created_at)
    WHERE status IN ('queued', 'running', 'waiting_workflow', 'waiting_input');

CREATE INDEX project_control_commands_reconcile_idx
    ON project_control_commands(next_reconcile_at, status)
    WHERE status IN ('running', 'waiting_workflow', 'waiting_input');

ALTER TABLE review_runs
    ADD COLUMN project_control_command_id UUID REFERENCES project_control_commands(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX review_runs_project_control_command_idx
    ON review_runs(project_control_command_id)
    WHERE project_control_command_id IS NOT NULL;

ALTER TABLE review_fixes
    ADD COLUMN project_control_command_id UUID REFERENCES project_control_commands(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX review_fixes_project_control_command_idx
    ON review_fixes(project_control_command_id)
    WHERE project_control_command_id IS NOT NULL;

CREATE UNIQUE INDEX workflow_runs_project_control_origin_idx
    ON workflow_runs(
        project_id,
        workflow_type,
        ((input->'input'->>'projectControlCommandId'))
    )
    WHERE COALESCE(input->'input'->>'projectControlCommandId', '') <> '';

CREATE UNIQUE INDEX script_versions_project_control_origin_idx
    ON script_versions(project_id, ((metadata->>'projectControlCommandId')))
    WHERE COALESCE(metadata->>'projectControlCommandId', '') <> '';

ALTER TABLE agent_runs
    ADD COLUMN project_control_command_id UUID REFERENCES project_control_commands(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX agent_runs_project_control_task_idx
    ON agent_runs(project_control_command_id, task_type)
    WHERE project_control_command_id IS NOT NULL;

CREATE TABLE project_control_command_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    command_id UUID NOT NULL REFERENCES project_control_commands(id) ON DELETE CASCADE,
    item_key TEXT NOT NULL,
    stable_ordinal INTEGER,
    target_type TEXT NOT NULL,
    target_id UUID,
    target_revision BIGINT,
    input JSONB NOT NULL DEFAULT '{}'::jsonb,
    input_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    retryable BOOLEAN NOT NULL DEFAULT false,
    output JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code TEXT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT project_control_command_items_item_key_nonempty CHECK (btrim(item_key) <> ''),
    CONSTRAINT project_control_command_items_stable_ordinal_positive CHECK (
        stable_ordinal IS NULL OR stable_ordinal > 0
    ),
    CONSTRAINT project_control_command_items_target_type_nonempty CHECK (btrim(target_type) <> ''),
    CONSTRAINT project_control_command_items_target_revision_positive CHECK (
        target_revision IS NULL OR target_revision > 0
    ),
    CONSTRAINT project_control_command_items_input_object_check CHECK (jsonb_typeof(input) = 'object'),
    CONSTRAINT project_control_command_items_input_size_check CHECK (octet_length(input::text) <= 65536),
    CONSTRAINT project_control_command_items_input_hash_sha256 CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT project_control_command_items_status_check CHECK (
        status IN ('queued', 'running', 'waiting_workflow', 'succeeded', 'failed', 'cancelled', 'skipped')
    ),
    CONSTRAINT project_control_command_items_output_object_check CHECK (jsonb_typeof(output) = 'object'),
    CONSTRAINT project_control_command_items_output_size_check CHECK (octet_length(output::text) <= 65536),
    CONSTRAINT project_control_command_items_failure_error_check CHECK (
        status <> 'failed' OR (error_code IS NOT NULL AND btrim(error_code) <> '')
    ),
    CONSTRAINT project_control_command_items_terminal_time_check CHECK (
        (status IN ('succeeded', 'failed', 'cancelled', 'skipped')) = (completed_at IS NOT NULL)
    ),
    UNIQUE (command_id, item_key),
    UNIQUE (id, command_id)
);

CREATE UNIQUE INDEX project_control_command_items_stable_ordinal_idx
    ON project_control_command_items(command_id, stable_ordinal)
    WHERE stable_ordinal IS NOT NULL;

CREATE INDEX project_control_command_items_command_status_idx
    ON project_control_command_items(command_id, status, stable_ordinal, created_at);

CREATE TABLE project_control_command_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    command_id UUID NOT NULL REFERENCES project_control_commands(id) ON DELETE CASCADE,
    command_item_id UUID,
    attempt_number INTEGER NOT NULL,
    attempt_kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running',
    worker_release_id TEXT NOT NULL,
    lease_identity TEXT NOT NULL,
    error_code TEXT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT project_control_command_attempts_number_positive CHECK (attempt_number > 0),
    CONSTRAINT project_control_command_attempts_kind_check CHECK (
        attempt_kind IN ('dispatch', 'reconcile', 'automatic_retry')
    ),
    CONSTRAINT project_control_command_attempts_status_check CHECK (
        status IN ('running', 'succeeded', 'failed', 'cancelled')
    ),
    CONSTRAINT project_control_command_attempts_identity_nonempty CHECK (
        btrim(worker_release_id) <> '' AND btrim(lease_identity) <> ''
    ),
    CONSTRAINT project_control_command_attempts_failure_error_check CHECK (
        status <> 'failed' OR (error_code IS NOT NULL AND btrim(error_code) <> '')
    ),
    CONSTRAINT project_control_command_attempts_terminal_time_check CHECK (
        (status IN ('succeeded', 'failed', 'cancelled')) = (completed_at IS NOT NULL)
    ),
    CONSTRAINT project_control_command_attempts_item_command_fk
        FOREIGN KEY (command_item_id, command_id)
        REFERENCES project_control_command_items(id, command_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX project_control_command_attempts_command_number_idx
    ON project_control_command_attempts(command_id, attempt_kind, attempt_number)
    WHERE command_item_id IS NULL;

CREATE UNIQUE INDEX project_control_command_attempts_item_number_idx
    ON project_control_command_attempts(command_item_id, attempt_kind, attempt_number)
    WHERE command_item_id IS NOT NULL;

CREATE INDEX project_control_command_attempts_active_idx
    ON project_control_command_attempts(command_id, status, created_at DESC);

CREATE TABLE project_control_command_workflows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    command_id UUID NOT NULL REFERENCES project_control_commands(id) ON DELETE CASCADE,
    command_item_id UUID,
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    temporal_workflow_id TEXT NOT NULL,
    temporal_run_id TEXT,
    relation_type TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_control_command_workflows_temporal_id_nonempty CHECK (btrim(temporal_workflow_id) <> ''),
    CONSTRAINT project_control_command_workflows_relation_nonempty CHECK (btrim(relation_type) <> ''),
    CONSTRAINT project_control_command_workflows_item_command_fk
        FOREIGN KEY (command_item_id, command_id)
        REFERENCES project_control_command_items(id, command_id) ON DELETE CASCADE,
    UNIQUE (command_id, temporal_workflow_id)
);

CREATE UNIQUE INDEX project_control_command_workflows_run_idx
    ON project_control_command_workflows(command_id, workflow_run_id)
    WHERE workflow_run_id IS NOT NULL;

CREATE INDEX project_control_command_workflows_item_idx
    ON project_control_command_workflows(command_item_id, created_at)
    WHERE command_item_id IS NOT NULL;

CREATE TABLE project_control_command_prompts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    command_id UUID NOT NULL REFERENCES project_control_commands(id) ON DELETE CASCADE,
    prompt_kind TEXT NOT NULL,
    prompt TEXT NOT NULL,
    options JSONB NOT NULL DEFAULT '[]'::jsonb,
    status TEXT NOT NULL DEFAULT 'pending',
    expected_command_revision BIGINT NOT NULL,
    candidate_revisions JSONB NOT NULL DEFAULT '{}'::jsonb,
    expires_at TIMESTAMPTZ NOT NULL,
    answer JSONB,
    answer_idempotency_key TEXT,
    answered_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    answered_at TIMESTAMPTZ,
    CONSTRAINT project_control_command_prompts_kind_nonempty CHECK (btrim(prompt_kind) <> ''),
    CONSTRAINT project_control_command_prompts_prompt_nonempty CHECK (btrim(prompt) <> ''),
    CONSTRAINT project_control_command_prompts_options_array_check CHECK (jsonb_typeof(options) = 'array'),
    CONSTRAINT project_control_command_prompts_options_size_check CHECK (octet_length(options::text) <= 32768),
    CONSTRAINT project_control_command_prompts_status_check CHECK (
        status IN ('pending', 'answered', 'expired', 'cancelled')
    ),
    CONSTRAINT project_control_command_prompts_revision_positive CHECK (expected_command_revision > 0),
    CONSTRAINT project_control_command_prompts_candidate_revisions_object_check CHECK (
        jsonb_typeof(candidate_revisions) = 'object'
    ),
    CONSTRAINT project_control_command_prompts_answer_idempotency_size_check CHECK (
        answer_idempotency_key IS NULL OR char_length(answer_idempotency_key) BETWEEN 1 AND 200
    ),
    CONSTRAINT project_control_command_prompts_answer_consistent CHECK (
        (status = 'answered' AND answer IS NOT NULL AND answer_idempotency_key IS NOT NULL
            AND answered_by_user_id IS NOT NULL AND answered_at IS NOT NULL)
        OR (status <> 'answered' AND answer IS NULL AND answer_idempotency_key IS NULL
            AND answered_by_user_id IS NULL AND answered_at IS NULL)
    )
);

CREATE UNIQUE INDEX project_control_command_prompts_one_pending_idx
    ON project_control_command_prompts(command_id)
    WHERE status = 'pending';

CREATE UNIQUE INDEX project_control_command_prompts_answer_idempotency_idx
    ON project_control_command_prompts(command_id, answer_idempotency_key)
    WHERE answer_idempotency_key IS NOT NULL;

CREATE INDEX project_control_command_prompts_expiry_idx
    ON project_control_command_prompts(expires_at)
    WHERE status = 'pending';

CREATE TABLE project_control_command_events (
    sequence BIGSERIAL PRIMARY KEY,
    command_id UUID NOT NULL REFERENCES project_control_commands(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_control_command_events_type_nonempty CHECK (btrim(event_type) <> ''),
    CONSTRAINT project_control_command_events_payload_object_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT project_control_command_events_payload_size_check CHECK (octet_length(payload::text) <= 32768)
);

CREATE INDEX project_control_command_events_command_cursor_idx
    ON project_control_command_events(command_id, sequence);

CREATE TABLE project_control_content_uploads (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    control_key_id UUID REFERENCES user_control_keys(id) ON DELETE SET NULL,
    target_type TEXT NOT NULL,
    target_id UUID NOT NULL,
    target_revision BIGINT NOT NULL,
    content_hash TEXT NOT NULL,
    content_format TEXT NOT NULL,
    expected_size_bytes BIGINT NOT NULL,
    expected_chunk_count INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'open',
    storage_prefix TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    committed_command_id UUID REFERENCES project_control_commands(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    committed_at TIMESTAMPTZ,
    aborted_at TIMESTAMPTZ,
    CONSTRAINT project_control_content_uploads_scope_check CHECK (project_id IS NULL OR workspace_id IS NOT NULL),
    CONSTRAINT project_control_content_uploads_target_type_nonempty CHECK (btrim(target_type) <> ''),
    CONSTRAINT project_control_content_uploads_target_revision_positive CHECK (target_revision > 0),
    CONSTRAINT project_control_content_uploads_content_hash_sha256 CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT project_control_content_uploads_format_nonempty CHECK (btrim(content_format) <> ''),
    CONSTRAINT project_control_content_uploads_size_check CHECK (
        expected_size_bytes BETWEEN 1 AND 1073741824
    ),
    CONSTRAINT project_control_content_uploads_chunk_count_check CHECK (
        expected_chunk_count BETWEEN 1 AND 10000
    ),
    CONSTRAINT project_control_content_uploads_status_check CHECK (
        status IN ('open', 'committed', 'aborted', 'expired')
    ),
    CONSTRAINT project_control_content_uploads_storage_prefix_nonempty CHECK (btrim(storage_prefix) <> ''),
    CONSTRAINT project_control_content_uploads_terminal_time_check CHECK (
        (status = 'committed' AND committed_at IS NOT NULL AND committed_command_id IS NOT NULL AND aborted_at IS NULL)
        OR (status IN ('aborted', 'expired') AND aborted_at IS NOT NULL AND committed_at IS NULL AND committed_command_id IS NULL)
        OR (status = 'open' AND committed_at IS NULL AND aborted_at IS NULL AND committed_command_id IS NULL)
    )
);

CREATE INDEX project_control_content_uploads_expiry_idx
    ON project_control_content_uploads(expires_at)
    WHERE status = 'open';

CREATE INDEX project_control_content_uploads_actor_idx
    ON project_control_content_uploads(actor_user_id, created_at DESC);

CREATE TABLE project_control_content_upload_chunks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    upload_id UUID NOT NULL REFERENCES project_control_content_uploads(id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL,
    byte_size INTEGER NOT NULL,
    content_hash TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_control_content_upload_chunks_index_nonnegative CHECK (chunk_index >= 0),
    CONSTRAINT project_control_content_upload_chunks_size_positive CHECK (byte_size > 0),
    CONSTRAINT project_control_content_upload_chunks_hash_sha256 CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT project_control_content_upload_chunks_storage_key_nonempty CHECK (btrim(storage_key) <> ''),
    UNIQUE (upload_id, chunk_index)
);

ALTER TABLE agent_steps
    ADD COLUMN project_control_command_id UUID REFERENCES project_control_commands(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX agent_steps_project_control_command_idx
    ON agent_steps(project_control_command_id)
    WHERE project_control_command_id IS NOT NULL;

CREATE TRIGGER project_control_commands_set_updated_at
    BEFORE UPDATE ON project_control_commands
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose StatementBegin
CREATE FUNCTION enforce_project_control_command_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status IN ('succeeded', 'partial_succeeded', 'failed', 'cancelled') THEN
        RAISE EXCEPTION 'terminal project control command is immutable'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.revision <> OLD.revision + 1 THEN
        RAISE EXCEPTION 'project control command revision must increment by one'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.status = OLD.status THEN
        RETURN NEW;
    END IF;
    IF NOT (
        (OLD.status = 'queued' AND NEW.status IN ('running', 'failed', 'cancelled'))
        OR (OLD.status = 'running' AND NEW.status IN ('queued', 'waiting_workflow', 'waiting_input', 'succeeded', 'partial_succeeded', 'failed', 'cancelled'))
        OR (OLD.status = 'waiting_workflow' AND NEW.status IN ('running', 'waiting_input', 'succeeded', 'partial_succeeded', 'failed', 'cancelled'))
        OR (OLD.status = 'waiting_input' AND NEW.status IN ('queued', 'running', 'failed', 'cancelled'))
    ) THEN
        RAISE EXCEPTION 'invalid project control command transition % -> %', OLD.status, NEW.status
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_control_commands_transition_guard
    BEFORE UPDATE ON project_control_commands
    FOR EACH ROW EXECUTE FUNCTION enforce_project_control_command_transition();

-- +goose StatementBegin
CREATE FUNCTION enforce_project_control_item_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.command_id IS DISTINCT FROM OLD.command_id
       OR NEW.item_key IS DISTINCT FROM OLD.item_key
       OR NEW.stable_ordinal IS DISTINCT FROM OLD.stable_ordinal
       OR NEW.target_type IS DISTINCT FROM OLD.target_type
       OR NEW.target_id IS DISTINCT FROM OLD.target_id
       OR NEW.target_revision IS DISTINCT FROM OLD.target_revision
       OR NEW.input_hash IS DISTINCT FROM OLD.input_hash THEN
        RAISE EXCEPTION 'project control command item snapshot is immutable'
            USING ERRCODE = '23514';
    END IF;
    IF OLD.status IN ('succeeded', 'failed', 'cancelled', 'skipped') THEN
        RAISE EXCEPTION 'terminal project control command item is immutable'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_control_command_items_identity_guard
    BEFORE UPDATE ON project_control_command_items
    FOR EACH ROW EXECUTE FUNCTION enforce_project_control_item_identity();

-- +goose StatementBegin
CREATE FUNCTION enforce_project_control_waiting_input()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    command_id_value UUID;
    command_status_value TEXT;
    pending_count INTEGER;
BEGIN
    IF TG_TABLE_NAME = 'project_control_commands' THEN
        command_id_value := NEW.id;
        command_status_value := NEW.status;
    ELSIF TG_OP = 'DELETE' THEN
        command_id_value := OLD.command_id;
        SELECT status INTO command_status_value
        FROM project_control_commands
        WHERE id = command_id_value;
    ELSE
        command_id_value := NEW.command_id;
        SELECT status INTO command_status_value
        FROM project_control_commands
        WHERE id = command_id_value;
    END IF;

    SELECT count(*) INTO pending_count
    FROM project_control_command_prompts
    WHERE command_id = command_id_value AND status = 'pending';

    IF command_status_value = 'waiting_input' AND pending_count <> 1 THEN
        RAISE EXCEPTION 'waiting_input command requires exactly one pending prompt'
            USING ERRCODE = '23514';
    END IF;
    IF command_status_value <> 'waiting_input' AND pending_count <> 0 THEN
        RAISE EXCEPTION 'pending prompt requires waiting_input command status'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER project_control_commands_waiting_input_guard
    AFTER INSERT OR UPDATE OF status ON project_control_commands
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION enforce_project_control_waiting_input();

CREATE CONSTRAINT TRIGGER project_control_prompts_waiting_input_guard
    AFTER INSERT OR UPDATE OF status OR DELETE ON project_control_command_prompts
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION enforce_project_control_waiting_input();

-- +goose Down
SET search_path TO public;

DROP TRIGGER IF EXISTS project_control_prompts_waiting_input_guard ON project_control_command_prompts;
DROP TRIGGER IF EXISTS project_control_commands_waiting_input_guard ON project_control_commands;
DROP FUNCTION IF EXISTS enforce_project_control_waiting_input();

DROP TRIGGER IF EXISTS project_control_command_items_identity_guard ON project_control_command_items;
DROP FUNCTION IF EXISTS enforce_project_control_item_identity();

DROP TRIGGER IF EXISTS project_control_commands_transition_guard ON project_control_commands;
DROP FUNCTION IF EXISTS enforce_project_control_command_transition();

DROP TRIGGER IF EXISTS project_control_commands_set_updated_at ON project_control_commands;

DROP INDEX IF EXISTS agent_steps_project_control_command_idx;
ALTER TABLE agent_steps DROP COLUMN IF EXISTS project_control_command_id;

DROP INDEX IF EXISTS review_fixes_project_control_command_idx;
ALTER TABLE review_fixes DROP COLUMN IF EXISTS project_control_command_id;
DROP INDEX IF EXISTS review_runs_project_control_command_idx;
ALTER TABLE review_runs DROP COLUMN IF EXISTS project_control_command_id;
DROP INDEX IF EXISTS workflow_runs_project_control_origin_idx;
DROP INDEX IF EXISTS script_versions_project_control_origin_idx;
DROP INDEX IF EXISTS agent_runs_project_control_task_idx;
ALTER TABLE agent_runs DROP COLUMN IF EXISTS project_control_command_id;

DROP TABLE IF EXISTS project_control_content_upload_chunks;
DROP TABLE IF EXISTS project_control_content_uploads;
DROP TABLE IF EXISTS project_control_command_events;
DROP TABLE IF EXISTS project_control_command_prompts;
DROP TABLE IF EXISTS project_control_command_workflows;
DROP TABLE IF EXISTS project_control_command_attempts;
DROP TABLE IF EXISTS project_control_command_items;
DROP TABLE IF EXISTS project_control_commands;
DROP TABLE IF EXISTS user_control_keys;

DROP TRIGGER IF EXISTS timeline_clips_maintain_revision ON timeline_clips;
DROP FUNCTION IF EXISTS maintain_timeline_clip_revision();
ALTER TABLE timeline_clips
    DROP CONSTRAINT IF EXISTS timeline_clips_revision_positive,
    DROP COLUMN IF EXISTS revision;

DROP TRIGGER IF EXISTS project_timelines_maintain_revision ON project_timelines;
DROP FUNCTION IF EXISTS maintain_project_timeline_revision();

DROP TRIGGER IF EXISTS final_video_versions_maintain_revision ON final_video_versions;
DROP FUNCTION IF EXISTS maintain_final_video_version_revision();
ALTER TABLE final_video_versions
    DROP CONSTRAINT IF EXISTS final_video_versions_revision_positive,
    DROP COLUMN IF EXISTS revision;

DROP TRIGGER IF EXISTS character_voice_profiles_maintain_revision ON character_voice_profiles;
DROP FUNCTION IF EXISTS maintain_character_voice_profile_revision();
ALTER TABLE character_voice_profiles
    DROP CONSTRAINT IF EXISTS character_voice_profiles_revision_positive,
    DROP COLUMN IF EXISTS revision;

DROP TRIGGER IF EXISTS adaptation_plans_maintain_revision ON adaptation_plans;
DROP FUNCTION IF EXISTS maintain_adaptation_plan_revision();
ALTER TABLE adaptation_plans
    DROP CONSTRAINT IF EXISTS adaptation_plans_revision_positive,
    DROP COLUMN IF EXISTS revision;

DROP TRIGGER IF EXISTS novel_events_maintain_revision ON novel_events;
DROP FUNCTION IF EXISTS maintain_novel_event_revision();
ALTER TABLE novel_events
    DROP CONSTRAINT IF EXISTS novel_events_revision_positive,
    DROP COLUMN IF EXISTS revision;

DROP TRIGGER IF EXISTS script_scenes_maintain_revision ON script_scenes;
DROP FUNCTION IF EXISTS maintain_script_scene_revision();
ALTER TABLE script_scenes
    DROP CONSTRAINT IF EXISTS script_scenes_revision_positive,
    DROP COLUMN IF EXISTS revision;

DROP TRIGGER IF EXISTS project_sources_maintain_revision ON project_sources;
DROP FUNCTION IF EXISTS maintain_project_source_revision();
ALTER TABLE project_sources
    DROP CONSTRAINT IF EXISTS project_sources_revision_positive,
    DROP COLUMN IF EXISTS revision;
