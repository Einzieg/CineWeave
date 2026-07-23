-- +goose Up

SET search_path TO public;

ALTER TABLE project_video_production_rebuilds
    ADD COLUMN source_commerce_workflow_binding_id UUID,
    ADD COLUMN target_commerce_workflow_binding_id UUID,
    ADD COLUMN source_commerce_configuration_hash TEXT,
    ADD COLUMN target_commerce_configuration_hash TEXT,
    ADD CONSTRAINT project_rebuilds_source_commerce_binding_fk
        FOREIGN KEY (source_commerce_workflow_binding_id, project_id, organization_id)
        REFERENCES project_commerce_workflow_bindings(id, project_id, organization_id) ON DELETE RESTRICT,
    ADD CONSTRAINT project_rebuilds_target_commerce_binding_fk
        FOREIGN KEY (target_commerce_workflow_binding_id, project_id, organization_id)
        REFERENCES project_commerce_workflow_bindings(id, project_id, organization_id) ON DELETE RESTRICT,
    ADD CONSTRAINT project_rebuilds_source_commerce_hash_check CHECK (
        source_commerce_configuration_hash IS NULL
        OR source_commerce_configuration_hash ~ '^[0-9a-f]{64}$'
    ),
    ADD CONSTRAINT project_rebuilds_target_commerce_hash_check CHECK (
        target_commerce_configuration_hash IS NULL
        OR target_commerce_configuration_hash ~ '^[0-9a-f]{64}$'
    );

-- +goose StatementBegin
CREATE FUNCTION validate_project_rebuild_commerce_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    selected_kind TEXT;
    source_binding UUID;
    source_hash TEXT;
    target_binding UUID;
    target_hash TEXT;
BEGIN
    SELECT project_kind INTO selected_kind
    FROM projects
    WHERE id = NEW.project_id AND organization_id = NEW.organization_id;

    IF selected_kind = 'narrative' THEN
        IF NEW.source_commerce_workflow_binding_id IS NOT NULL
           OR NEW.target_commerce_workflow_binding_id IS NOT NULL
           OR NEW.source_commerce_configuration_hash IS NOT NULL
           OR NEW.target_commerce_configuration_hash IS NOT NULL THEN
            RAISE EXCEPTION 'narrative rebuild cannot carry commerce binding identity' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    SELECT generation.commerce_workflow_binding_id, binding.configuration_hash
    INTO source_binding, source_hash
    FROM project_video_production_generations generation
    JOIN project_commerce_workflow_bindings binding
      ON binding.id = generation.commerce_workflow_binding_id
     AND binding.project_id = generation.project_id
     AND binding.organization_id = generation.organization_id
    WHERE generation.id = NEW.source_generation_id
      AND generation.project_id = NEW.project_id
      AND generation.organization_id = NEW.organization_id;

    IF source_binding IS NULL
       OR source_binding IS DISTINCT FROM NEW.source_commerce_workflow_binding_id
       OR source_hash IS DISTINCT FROM NEW.source_commerce_configuration_hash THEN
        RAISE EXCEPTION 'commerce rebuild source binding identity mismatch' USING ERRCODE = '23514';
    END IF;

    IF NEW.target_generation_id IS NULL THEN
        IF NEW.target_commerce_workflow_binding_id IS NOT NULL
           OR NEW.target_commerce_configuration_hash IS NOT NULL THEN
            RAISE EXCEPTION 'commerce rebuild target binding requires a target generation' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    SELECT generation.commerce_workflow_binding_id, binding.configuration_hash
    INTO target_binding, target_hash
    FROM project_video_production_generations generation
    JOIN project_commerce_workflow_bindings binding
      ON binding.id = generation.commerce_workflow_binding_id
     AND binding.project_id = generation.project_id
     AND binding.organization_id = generation.organization_id
    WHERE generation.id = NEW.target_generation_id
      AND generation.project_id = NEW.project_id
      AND generation.organization_id = NEW.organization_id;

    IF target_binding IS NULL
       OR target_binding IS DISTINCT FROM NEW.target_commerce_workflow_binding_id
       OR target_hash IS DISTINCT FROM NEW.target_commerce_configuration_hash THEN
        RAISE EXCEPTION 'commerce rebuild target binding identity mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_rebuilds_commerce_identity
BEFORE INSERT OR UPDATE OF organization_id, project_id, source_generation_id, target_generation_id,
    source_commerce_workflow_binding_id, target_commerce_workflow_binding_id,
    source_commerce_configuration_hash, target_commerce_configuration_hash
ON project_video_production_rebuilds
FOR EACH ROW EXECUTE FUNCTION validate_project_rebuild_commerce_identity();

CREATE TABLE commerce_script_unit_batch_coordinators (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    project_production_generation_id UUID NOT NULL,
    target_stage TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    idempotency_scope TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    input_snapshot JSONB NOT NULL,
    max_concurrency INTEGER NOT NULL DEFAULT 4,
    retry_of_coordinator_id UUID REFERENCES commerce_script_unit_batch_coordinators(id) ON DELETE SET NULL,
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    total_items INTEGER NOT NULL DEFAULT 0,
    completed_items INTEGER NOT NULL DEFAULT 0,
    failed_items INTEGER NOT NULL DEFAULT 0,
    cancelled_items INTEGER NOT NULL DEFAULT 0,
    requested_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    error_code TEXT,
    error_message TEXT,
    CONSTRAINT commerce_batch_coordinators_generation_fk
        FOREIGN KEY (project_production_generation_id, project_id, organization_id)
        REFERENCES project_video_production_generations(id, project_id, organization_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_batch_coordinators_stage_check
        CHECK (target_stage IN ('storyboard', 'reference_images', 'video_prompts', 'shot_videos', 'final_compose')),
    CONSTRAINT commerce_batch_coordinators_status_check
        CHECK (status IN ('queued', 'running', 'partially_succeeded', 'succeeded', 'failed', 'cancelling', 'cancelled')),
    CONSTRAINT commerce_batch_coordinators_scope_check CHECK (trim(idempotency_scope) <> ''),
    CONSTRAINT commerce_batch_coordinators_key_check CHECK (trim(idempotency_key) <> ''),
    CONSTRAINT commerce_batch_coordinators_hash_check CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_batch_coordinators_input_check CHECK (jsonb_typeof(input_snapshot) = 'object'),
    CONSTRAINT commerce_batch_coordinators_concurrency_check CHECK (max_concurrency BETWEEN 1 AND 16),
    CONSTRAINT commerce_batch_coordinators_revision_check CHECK (revision > 0),
    CONSTRAINT commerce_batch_coordinators_counts_check CHECK (
        total_items >= 0 AND completed_items >= 0 AND failed_items >= 0 AND cancelled_items >= 0
        AND completed_items + failed_items + cancelled_items <= total_items
    ),
    CONSTRAINT commerce_batch_coordinators_terminal_check CHECK (
        (status IN ('partially_succeeded', 'succeeded', 'failed') AND completed_at IS NOT NULL AND cancelled_at IS NULL)
        OR (status = 'cancelled' AND completed_at IS NOT NULL AND cancelled_at IS NOT NULL)
        OR (status NOT IN ('partially_succeeded', 'succeeded', 'failed', 'cancelled') AND completed_at IS NULL)
    ),
    UNIQUE(organization_id, idempotency_scope, idempotency_key),
    UNIQUE(id, organization_id, project_id)
);

CREATE TABLE commerce_script_unit_batch_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    coordinator_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    script_unit_generation_id UUID NOT NULL,
    child_run_id UUID,
    child_workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    input_snapshot JSONB NOT NULL,
    attempt_generation INTEGER NOT NULL DEFAULT 1,
    ordinal INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    error_code TEXT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT commerce_batch_items_coordinator_fk
        FOREIGN KEY (coordinator_id, organization_id, project_id)
        REFERENCES commerce_script_unit_batch_coordinators(id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_batch_items_generation_fk
        FOREIGN KEY (script_unit_generation_id, script_unit_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_batch_items_ordinal_check CHECK (ordinal >= 0),
    CONSTRAINT commerce_batch_items_input_check CHECK (jsonb_typeof(input_snapshot) = 'object'),
    CONSTRAINT commerce_batch_items_attempt_generation_check CHECK (attempt_generation > 0),
    CONSTRAINT commerce_batch_items_status_check
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'skipped')),
    CONSTRAINT commerce_batch_items_terminal_check CHECK (
        (status IN ('succeeded', 'failed', 'cancelled', 'skipped') AND completed_at IS NOT NULL)
        OR (status NOT IN ('succeeded', 'failed', 'cancelled', 'skipped') AND completed_at IS NULL)
    ),
    UNIQUE(coordinator_id, ordinal),
    UNIQUE(coordinator_id, script_unit_id),
    UNIQUE(child_run_id),
    UNIQUE(child_workflow_run_id),
    UNIQUE(id, organization_id, project_id)
);

CREATE INDEX commerce_batch_coordinators_project_status_idx
    ON commerce_script_unit_batch_coordinators(project_id, status, created_at DESC);

CREATE INDEX commerce_batch_coordinators_retry_idx
    ON commerce_script_unit_batch_coordinators(retry_of_coordinator_id)
    WHERE retry_of_coordinator_id IS NOT NULL;

CREATE INDEX commerce_batch_items_status_idx
    ON commerce_script_unit_batch_items(coordinator_id, status, ordinal);

CREATE TABLE commerce_production_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    script_unit_generation_id UUID NOT NULL,
    project_production_generation_id UUID NOT NULL,
    run_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    idempotency_scope TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    input_snapshot JSONB NOT NULL,
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    coordinator_item_id UUID,
    revision BIGINT NOT NULL DEFAULT 1,
    total_items INTEGER NOT NULL DEFAULT 0,
    completed_items INTEGER NOT NULL DEFAULT 0,
    failed_items INTEGER NOT NULL DEFAULT 0,
    cancelled_items INTEGER NOT NULL DEFAULT 0,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    error_code TEXT,
    error_message TEXT,
    CONSTRAINT commerce_production_runs_unit_generation_fk
        FOREIGN KEY (script_unit_generation_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_production_runs_project_generation_fk
        FOREIGN KEY (project_production_generation_id, project_id, organization_id)
        REFERENCES project_video_production_generations(id, project_id, organization_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_production_runs_coordinator_item_fk
        FOREIGN KEY (coordinator_item_id, organization_id, project_id)
        REFERENCES commerce_script_unit_batch_items(id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_production_runs_type_check
        CHECK (run_type IN ('storyboard_plan', 'reference_images', 'video_prompts', 'shot_videos', 'final_compose')),
    CONSTRAINT commerce_production_runs_status_check
        CHECK (status IN ('queued', 'running', 'partially_succeeded', 'succeeded', 'failed', 'cancelling', 'cancelled')),
    CONSTRAINT commerce_production_runs_scope_check CHECK (trim(idempotency_scope) <> ''),
    CONSTRAINT commerce_production_runs_key_check CHECK (trim(idempotency_key) <> ''),
    CONSTRAINT commerce_production_runs_payload_hash_check CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_production_runs_input_check CHECK (jsonb_typeof(input_snapshot) = 'object'),
    CONSTRAINT commerce_production_runs_revision_check CHECK (revision > 0),
    CONSTRAINT commerce_production_runs_counts_check CHECK (
        total_items >= 0 AND completed_items >= 0 AND failed_items >= 0 AND cancelled_items >= 0
        AND completed_items + failed_items + cancelled_items <= total_items
    ),
    CONSTRAINT commerce_production_runs_terminal_check CHECK (
        (status IN ('partially_succeeded', 'succeeded', 'failed') AND completed_at IS NOT NULL AND cancelled_at IS NULL)
        OR (status = 'cancelled' AND completed_at IS NOT NULL AND cancelled_at IS NOT NULL)
        OR (status NOT IN ('partially_succeeded', 'succeeded', 'failed', 'cancelled') AND completed_at IS NULL)
    ),
    UNIQUE(organization_id, idempotency_scope, idempotency_key),
    UNIQUE(id, script_unit_id, script_unit_generation_id, organization_id, project_id),
    UNIQUE(coordinator_item_id)
);

ALTER TABLE commerce_script_unit_batch_items
    ADD CONSTRAINT commerce_batch_items_child_run_fk
        FOREIGN KEY (child_run_id, script_unit_id, script_unit_generation_id, organization_id, project_id)
        REFERENCES commerce_production_runs(id, script_unit_id, script_unit_generation_id, organization_id, project_id) ON DELETE RESTRICT;

CREATE INDEX commerce_production_runs_unit_status_idx
    ON commerce_production_runs(script_unit_id, script_unit_generation_id, status, created_at DESC);

CREATE TABLE commerce_production_run_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    run_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    script_unit_generation_id UUID NOT NULL,
    subject_type TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    storyboard_shot_id UUID,
    input_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    current_attempt INTEGER NOT NULL DEFAULT 0,
    output_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    output_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
    output_media_file_id UUID REFERENCES media_files(id) ON DELETE SET NULL,
    output_storyboard_plan_id UUID REFERENCES commerce_storyboard_plans(id) ON DELETE SET NULL,
    output_video_prompt_plan_id UUID REFERENCES video_prompt_plans(id) ON DELETE SET NULL,
    output_video_render_plan_id UUID REFERENCES video_render_plans(id) ON DELETE SET NULL,
    output_final_video_version_id UUID REFERENCES final_video_versions(id) ON DELETE SET NULL,
    error_code TEXT,
    error_message TEXT,
    retryable BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_run_items_run_fk
        FOREIGN KEY (run_id, script_unit_id, script_unit_generation_id, organization_id, project_id)
        REFERENCES commerce_production_runs(id, script_unit_id, script_unit_generation_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_run_items_shot_fk
        FOREIGN KEY (storyboard_shot_id)
        REFERENCES storyboard_shots(id) ON DELETE RESTRICT,
    CONSTRAINT commerce_run_items_subject_type_check
        CHECK (subject_type IN ('plan_phase', 'candidate_shot', 'storyboard_shot', 'final_compose')),
    CONSTRAINT commerce_run_items_subject_key_check CHECK (trim(subject_key) <> ''),
    CONSTRAINT commerce_run_items_input_hash_check CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_run_items_status_check CHECK (
        status IN ('queued', 'running', 'succeeded', 'failed_retryable', 'failed_terminal', 'cancelled', 'discarded', 'skipped')
    ),
    CONSTRAINT commerce_run_items_attempt_check CHECK (current_attempt >= 0),
    CONSTRAINT commerce_run_items_output_check CHECK (jsonb_typeof(output_snapshot) = 'object'),
    CONSTRAINT commerce_run_items_failure_check CHECK (
        (status IN ('failed_retryable', 'failed_terminal') AND error_code IS NOT NULL)
        OR status NOT IN ('failed_retryable', 'failed_terminal')
    ),
    CONSTRAINT commerce_run_items_terminal_check CHECK (
        (status IN ('succeeded', 'failed_retryable', 'failed_terminal', 'cancelled', 'discarded', 'skipped') AND completed_at IS NOT NULL)
        OR (status NOT IN ('succeeded', 'failed_retryable', 'failed_terminal', 'cancelled', 'discarded', 'skipped') AND completed_at IS NULL)
    ),
    UNIQUE(run_id, subject_type, subject_key),
    UNIQUE(id, run_id, organization_id, project_id)
);

CREATE INDEX commerce_run_items_status_idx
    ON commerce_production_run_items(run_id, status, subject_type, subject_key);

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_run_item_subject()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    selected_run_type TEXT;
    selected_unit_generation UUID;
BEGIN
    SELECT run_type, script_unit_generation_id
    INTO selected_run_type, selected_unit_generation
    FROM commerce_production_runs
    WHERE id = NEW.run_id
      AND script_unit_id = NEW.script_unit_id
      AND script_unit_generation_id = NEW.script_unit_generation_id
      AND organization_id = NEW.organization_id
      AND project_id = NEW.project_id;

    IF selected_run_type = 'storyboard_plan' THEN
        IF NEW.subject_type NOT IN ('plan_phase', 'candidate_shot') OR NEW.storyboard_shot_id IS NOT NULL THEN
            RAISE EXCEPTION 'storyboard planning run item has invalid subject' USING ERRCODE = '23514';
        END IF;
    ELSIF selected_run_type = 'final_compose' THEN
        IF NEW.subject_type <> 'final_compose' OR NEW.storyboard_shot_id IS NOT NULL THEN
            RAISE EXCEPTION 'final compose run item has invalid subject' USING ERRCODE = '23514';
        END IF;
    ELSE
        IF NEW.subject_type <> 'storyboard_shot' OR NEW.storyboard_shot_id IS NULL THEN
            RAISE EXCEPTION 'shot production run item requires a storyboard shot subject' USING ERRCODE = '23514';
        END IF;
        IF NOT EXISTS (
            SELECT 1
            FROM storyboard_shots shot
            JOIN commerce_storyboard_plans plan ON plan.id = shot.commerce_storyboard_plan_id
            WHERE shot.id = NEW.storyboard_shot_id
              AND shot.project_id = NEW.project_id
              AND plan.script_unit_generation_id = selected_unit_generation
        ) THEN
            RAISE EXCEPTION 'run item shot does not belong to the script unit generation' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_run_items_typed_subject
BEFORE INSERT OR UPDATE OF run_id, script_unit_id, script_unit_generation_id,
    organization_id, project_id, subject_type, storyboard_shot_id
ON commerce_production_run_items
FOR EACH ROW EXECUTE FUNCTION validate_commerce_run_item_subject();

CREATE TABLE commerce_production_run_item_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    run_id UUID NOT NULL,
    item_id UUID NOT NULL,
    attempt_number INTEGER NOT NULL,
    input_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    node_run_id UUID REFERENCES workflow_node_runs(id) ON DELETE SET NULL,
    provider_request_id UUID REFERENCES provider_requests(id) ON DELETE SET NULL,
    provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    provider_async_task_id UUID REFERENCES provider_async_tasks(id) ON DELETE SET NULL,
    output_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    output_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
    output_media_file_id UUID REFERENCES media_files(id) ON DELETE SET NULL,
    output_storyboard_plan_id UUID REFERENCES commerce_storyboard_plans(id) ON DELETE SET NULL,
    output_video_prompt_plan_id UUID REFERENCES video_prompt_plans(id) ON DELETE SET NULL,
    output_video_render_plan_id UUID REFERENCES video_render_plans(id) ON DELETE SET NULL,
    output_final_video_version_id UUID REFERENCES final_video_versions(id) ON DELETE SET NULL,
    error_code TEXT,
    error_message TEXT,
    retryable BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT commerce_run_attempts_item_fk
        FOREIGN KEY (item_id, run_id, organization_id, project_id)
        REFERENCES commerce_production_run_items(id, run_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_run_attempts_number_check CHECK (attempt_number > 0),
    CONSTRAINT commerce_run_attempts_input_hash_check CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_run_attempts_status_check CHECK (
        status IN ('queued', 'running', 'succeeded', 'failed_retryable', 'failed_terminal', 'cancelled', 'discarded')
    ),
    CONSTRAINT commerce_run_attempts_output_check CHECK (jsonb_typeof(output_snapshot) = 'object'),
    CONSTRAINT commerce_run_attempts_failure_check CHECK (
        (status IN ('failed_retryable', 'failed_terminal') AND error_code IS NOT NULL)
        OR status NOT IN ('failed_retryable', 'failed_terminal')
    ),
    CONSTRAINT commerce_run_attempts_terminal_check CHECK (
        (status IN ('succeeded', 'failed_retryable', 'failed_terminal', 'cancelled', 'discarded') AND completed_at IS NOT NULL)
        OR (status NOT IN ('succeeded', 'failed_retryable', 'failed_terminal', 'cancelled', 'discarded') AND completed_at IS NULL)
    ),
    UNIQUE(item_id, attempt_number)
);

CREATE INDEX commerce_run_attempts_provider_idx
    ON commerce_production_run_item_attempts(provider_async_task_id, status)
    WHERE provider_async_task_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_run_attempt_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.organization_id IS DISTINCT FROM OLD.organization_id
       OR NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.run_id IS DISTINCT FROM OLD.run_id
       OR NEW.item_id IS DISTINCT FROM OLD.item_id
       OR NEW.attempt_number IS DISTINCT FROM OLD.attempt_number
       OR NEW.input_hash IS DISTINCT FROM OLD.input_hash
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'commerce production attempt identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status IN ('succeeded', 'failed_retryable', 'failed_terminal', 'cancelled', 'discarded') AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'terminal commerce production attempts are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_run_attempts_identity_immutable
BEFORE UPDATE ON commerce_production_run_item_attempts
FOR EACH ROW EXECUTE FUNCTION protect_commerce_run_attempt_identity();

CREATE TABLE commerce_product_rebuilds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    project_production_generation_id UUID NOT NULL,
    source_product_version_id UUID NOT NULL,
    target_product_version_id UUID NOT NULL,
    target_reference_set_hash TEXT NOT NULL,
    impact_snapshot JSONB NOT NULL,
    impact_token TEXT NOT NULL,
    expected_product_revision BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'planned',
    idempotency_key TEXT NOT NULL,
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    requested_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    error_code TEXT,
    error_message TEXT,
    CONSTRAINT commerce_product_rebuilds_product_fk
        FOREIGN KEY (product_id, organization_id, project_id)
        REFERENCES commerce_products(id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_product_rebuilds_generation_fk
        FOREIGN KEY (project_production_generation_id, project_id, organization_id)
        REFERENCES project_video_production_generations(id, project_id, organization_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_product_rebuilds_source_version_fk
        FOREIGN KEY (source_product_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_versions(id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_product_rebuilds_target_version_fk
        FOREIGN KEY (target_product_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_versions(id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_product_rebuilds_reference_hash_check CHECK (target_reference_set_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_product_rebuilds_impact_check CHECK (jsonb_typeof(impact_snapshot) = 'object'),
    CONSTRAINT commerce_product_rebuilds_token_check CHECK (impact_token ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_product_rebuilds_revision_check CHECK (expected_product_revision > 0),
    CONSTRAINT commerce_product_rebuilds_status_check
        CHECK (status IN ('planned', 'approved', 'preparing', 'running', 'blocked', 'succeeded', 'failed', 'cancelled')),
    CONSTRAINT commerce_product_rebuilds_key_check CHECK (trim(idempotency_key) <> ''),
    CONSTRAINT commerce_product_rebuilds_terminal_check CHECK (
        (status IN ('blocked', 'succeeded', 'failed', 'cancelled') AND completed_at IS NOT NULL)
        OR (status NOT IN ('blocked', 'succeeded', 'failed', 'cancelled') AND completed_at IS NULL)
    ),
    UNIQUE(organization_id, idempotency_key),
    UNIQUE(organization_id, impact_token),
    UNIQUE(id, product_id, organization_id, project_id)
);

CREATE TABLE commerce_product_rebuild_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    rebuild_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    source_unit_generation_id UUID NOT NULL,
    target_unit_generation_id UUID,
    source_reference_pack_id UUID NOT NULL,
    target_reference_pack_id UUID,
    status TEXT NOT NULL DEFAULT 'pending',
    blockers JSONB NOT NULL DEFAULT '[]'::jsonb,
    switched_at TIMESTAMPTZ,
    error_code TEXT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_product_rebuild_items_rebuild_fk
        FOREIGN KEY (rebuild_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_rebuilds(id, product_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_product_rebuild_items_source_generation_fk
        FOREIGN KEY (source_unit_generation_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_product_rebuild_items_target_generation_fk
        FOREIGN KEY (target_unit_generation_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_product_rebuild_items_source_pack_fk
        FOREIGN KEY (source_reference_pack_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_reference_packs(id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_product_rebuild_items_target_pack_fk
        FOREIGN KEY (target_reference_pack_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_reference_packs(id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_product_rebuild_items_status_check
        CHECK (status IN ('pending', 'blocked', 'ready', 'switched', 'failed', 'skipped')),
    CONSTRAINT commerce_product_rebuild_items_blockers_check CHECK (jsonb_typeof(blockers) = 'array'),
    CONSTRAINT commerce_product_rebuild_items_switch_check CHECK (
        (status = 'switched' AND switched_at IS NOT NULL AND target_unit_generation_id IS NOT NULL AND target_reference_pack_id IS NOT NULL)
        OR (status <> 'switched' AND switched_at IS NULL)
    ),
    UNIQUE(rebuild_id, script_unit_id)
);

CREATE TABLE commerce_project_rebuild_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    rebuild_id UUID NOT NULL REFERENCES project_video_production_rebuilds(id) ON DELETE CASCADE,
    script_unit_id UUID NOT NULL,
    source_unit_generation_id UUID NOT NULL,
    source_script_unit_revision BIGINT NOT NULL,
    target_unit_generation_id UUID,
    target_unit_configuration_hash TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    blockers JSONB NOT NULL DEFAULT '[]'::jsonb,
    checkpoint JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code TEXT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT commerce_project_rebuild_items_source_generation_fk
        FOREIGN KEY (source_unit_generation_id, script_unit_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_project_rebuild_items_target_generation_fk
        FOREIGN KEY (target_unit_generation_id, script_unit_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_project_rebuild_items_status_check
        CHECK (status IN ('pending', 'blocked', 'ready', 'switched', 'failed', 'skipped')),
    CONSTRAINT commerce_project_rebuild_items_source_revision_check CHECK (source_script_unit_revision > 0),
    CONSTRAINT commerce_project_rebuild_items_target_hash_check CHECK (
        (target_unit_generation_id IS NULL AND target_unit_configuration_hash IS NULL)
        OR (
            target_unit_generation_id IS NOT NULL
            AND target_unit_configuration_hash ~ '^[0-9a-f]{64}$'
        )
    ),
    CONSTRAINT commerce_project_rebuild_items_blockers_check CHECK (jsonb_typeof(blockers) = 'array'),
    CONSTRAINT commerce_project_rebuild_items_checkpoint_check CHECK (jsonb_typeof(checkpoint) = 'object'),
    CONSTRAINT commerce_project_rebuild_items_terminal_check CHECK (
        (status IN ('blocked', 'switched', 'failed', 'skipped') AND completed_at IS NOT NULL)
        OR (status NOT IN ('blocked', 'switched', 'failed', 'skipped') AND completed_at IS NULL)
    ),
    UNIQUE(rebuild_id, script_unit_id)
);

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_project_rebuild_item()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_generation UUID;
    target_hash TEXT;
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM project_video_production_rebuilds rebuild
        JOIN commerce_script_unit_generations source_generation
          ON source_generation.id = NEW.source_unit_generation_id
         AND source_generation.script_unit_id = NEW.script_unit_id
         AND source_generation.organization_id = NEW.organization_id
         AND source_generation.project_id = NEW.project_id
        WHERE rebuild.id = NEW.rebuild_id
          AND rebuild.organization_id = NEW.organization_id
          AND rebuild.project_id = NEW.project_id
          AND source_generation.project_production_generation_id = rebuild.source_generation_id
    ) THEN
        RAISE EXCEPTION 'commerce project rebuild item does not match source generation' USING ERRCODE = '23514';
    END IF;

    IF NEW.target_unit_generation_id IS NOT NULL THEN
        SELECT unit_generation.project_production_generation_id,
               unit_generation.unit_configuration_hash
        INTO target_generation, target_hash
        FROM project_video_production_rebuilds rebuild
        JOIN commerce_script_unit_generations unit_generation
          ON unit_generation.id = NEW.target_unit_generation_id
         AND unit_generation.script_unit_id = NEW.script_unit_id
         AND unit_generation.organization_id = NEW.organization_id
         AND unit_generation.project_id = NEW.project_id
        WHERE rebuild.id = NEW.rebuild_id
          AND rebuild.organization_id = NEW.organization_id
          AND rebuild.project_id = NEW.project_id
          AND rebuild.target_generation_id IS NOT NULL;

        IF target_generation IS NULL
           OR target_generation IS DISTINCT FROM (
               SELECT target_generation_id
               FROM project_video_production_rebuilds
               WHERE id = NEW.rebuild_id
           )
           OR target_hash IS DISTINCT FROM NEW.target_unit_configuration_hash THEN
            RAISE EXCEPTION 'commerce project rebuild item does not match target generation' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_project_rebuild_items_identity
BEFORE INSERT OR UPDATE OF organization_id, project_id, rebuild_id, script_unit_id,
    source_unit_generation_id, target_unit_generation_id, target_unit_configuration_hash
ON commerce_project_rebuild_items
FOR EACH ROW EXECUTE FUNCTION validate_commerce_project_rebuild_item();

ALTER TABLE project_timelines
    ADD COLUMN commerce_script_unit_id UUID,
    ADD COLUMN commerce_script_unit_generation_id UUID,
    ADD CONSTRAINT project_timelines_commerce_generation_fk
        FOREIGN KEY (commerce_script_unit_generation_id, commerce_script_unit_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, organization_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT project_timelines_commerce_pair_check CHECK (
        (commerce_script_unit_id IS NULL AND commerce_script_unit_generation_id IS NULL)
        OR (commerce_script_unit_id IS NOT NULL AND commerce_script_unit_generation_id IS NOT NULL)
    ),
    ADD CONSTRAINT project_timelines_commerce_identity_unique
        UNIQUE(id, project_id, production_generation_id, commerce_script_unit_id, commerce_script_unit_generation_id);

CREATE UNIQUE INDEX project_timelines_one_active_commerce_unit
    ON project_timelines(commerce_script_unit_generation_id)
    WHERE status = 'active' AND commerce_script_unit_generation_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_timeline_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    selected_kind TEXT;
    selected_project_generation UUID;
BEGIN
    SELECT project_kind INTO selected_kind FROM projects WHERE id = NEW.project_id;
    IF selected_kind = 'narrative' THEN
        IF NEW.commerce_script_unit_id IS NOT NULL THEN
            RAISE EXCEPTION 'narrative timeline cannot carry commerce identity' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    SELECT project_production_generation_id INTO selected_project_generation
    FROM commerce_script_unit_generations
    WHERE id = NEW.commerce_script_unit_generation_id
      AND script_unit_id = NEW.commerce_script_unit_id
      AND organization_id = NEW.organization_id
      AND project_id = NEW.project_id;
    IF selected_project_generation IS DISTINCT FROM NEW.production_generation_id THEN
        RAISE EXCEPTION 'commerce timeline generation identity mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_timelines_commerce_identity
BEFORE INSERT OR UPDATE OF organization_id, project_id, production_generation_id,
    commerce_script_unit_id, commerce_script_unit_generation_id
ON project_timelines
FOR EACH ROW EXECUTE FUNCTION validate_commerce_timeline_identity();

ALTER TABLE final_video_versions
    DROP CONSTRAINT final_video_versions_project_version_unique,
    ADD COLUMN commerce_script_unit_id UUID,
    ADD COLUMN commerce_script_unit_generation_id UUID,
    ADD CONSTRAINT final_video_versions_commerce_generation_fk
        FOREIGN KEY (commerce_script_unit_generation_id, commerce_script_unit_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, organization_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT final_video_versions_commerce_timeline_fk
        FOREIGN KEY (timeline_id, project_id, production_generation_id, commerce_script_unit_id, commerce_script_unit_generation_id)
        REFERENCES project_timelines(id, project_id, production_generation_id, commerce_script_unit_id, commerce_script_unit_generation_id) ON DELETE CASCADE,
    ADD CONSTRAINT final_video_versions_commerce_pair_check CHECK (
        (commerce_script_unit_id IS NULL AND commerce_script_unit_generation_id IS NULL)
        OR (commerce_script_unit_id IS NOT NULL AND commerce_script_unit_generation_id IS NOT NULL)
    );

CREATE UNIQUE INDEX final_video_versions_narrative_project_version_unique
    ON final_video_versions(project_id, version)
    WHERE commerce_script_unit_id IS NULL;

CREATE UNIQUE INDEX final_video_versions_commerce_unit_version_unique
    ON final_video_versions(commerce_script_unit_id, version)
    WHERE commerce_script_unit_id IS NOT NULL;

CREATE UNIQUE INDEX final_video_versions_one_active_commerce_unit
    ON final_video_versions(commerce_script_unit_id)
    WHERE commerce_script_unit_id IS NOT NULL AND status = 'active';

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_final_video_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    selected_kind TEXT;
BEGIN
    SELECT project_kind INTO selected_kind FROM projects WHERE id = NEW.project_id;
    IF selected_kind = 'narrative' THEN
        IF NEW.commerce_script_unit_id IS NOT NULL THEN
            RAISE EXCEPTION 'narrative final video cannot carry commerce identity' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.commerce_script_unit_id IS NULL OR NEW.commerce_script_unit_generation_id IS NULL THEN
        RAISE EXCEPTION 'commerce final video requires script unit generation identity' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER final_video_versions_commerce_identity
BEFORE INSERT OR UPDATE OF project_id, commerce_script_unit_id, commerce_script_unit_generation_id
ON final_video_versions
FOR EACH ROW EXECUTE FUNCTION validate_commerce_final_video_identity();

-- +goose Down

SET search_path TO public;

DROP TRIGGER IF EXISTS project_rebuilds_commerce_identity ON project_video_production_rebuilds;
DROP FUNCTION IF EXISTS validate_project_rebuild_commerce_identity();

ALTER TABLE project_video_production_rebuilds
    DROP CONSTRAINT IF EXISTS project_rebuilds_target_commerce_hash_check,
    DROP CONSTRAINT IF EXISTS project_rebuilds_source_commerce_hash_check,
    DROP CONSTRAINT IF EXISTS project_rebuilds_target_commerce_binding_fk,
    DROP CONSTRAINT IF EXISTS project_rebuilds_source_commerce_binding_fk,
    DROP COLUMN IF EXISTS target_commerce_configuration_hash,
    DROP COLUMN IF EXISTS source_commerce_configuration_hash,
    DROP COLUMN IF EXISTS target_commerce_workflow_binding_id,
    DROP COLUMN IF EXISTS source_commerce_workflow_binding_id;

DROP TRIGGER IF EXISTS final_video_versions_commerce_identity ON final_video_versions;
DROP FUNCTION IF EXISTS validate_commerce_final_video_identity();
DROP INDEX IF EXISTS final_video_versions_one_active_commerce_unit;
DROP INDEX IF EXISTS final_video_versions_commerce_unit_version_unique;
DROP INDEX IF EXISTS final_video_versions_narrative_project_version_unique;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM final_video_versions WHERE commerce_script_unit_id IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot restore project-level final video versions while commerce final videos exist';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE final_video_versions
    DROP CONSTRAINT IF EXISTS final_video_versions_commerce_pair_check,
    DROP CONSTRAINT IF EXISTS final_video_versions_commerce_timeline_fk,
    DROP CONSTRAINT IF EXISTS final_video_versions_commerce_generation_fk,
    DROP COLUMN IF EXISTS commerce_script_unit_generation_id,
    DROP COLUMN IF EXISTS commerce_script_unit_id,
    ADD CONSTRAINT final_video_versions_project_version_unique UNIQUE(project_id, version) DEFERRABLE;

DROP TRIGGER IF EXISTS project_timelines_commerce_identity ON project_timelines;
DROP FUNCTION IF EXISTS validate_commerce_timeline_identity();
DROP INDEX IF EXISTS project_timelines_one_active_commerce_unit;

ALTER TABLE project_timelines
    DROP CONSTRAINT IF EXISTS project_timelines_commerce_identity_unique,
    DROP CONSTRAINT IF EXISTS project_timelines_commerce_pair_check,
    DROP CONSTRAINT IF EXISTS project_timelines_commerce_generation_fk,
    DROP COLUMN IF EXISTS commerce_script_unit_generation_id,
    DROP COLUMN IF EXISTS commerce_script_unit_id;

DROP TRIGGER IF EXISTS commerce_project_rebuild_items_identity ON commerce_project_rebuild_items;
DROP FUNCTION IF EXISTS validate_commerce_project_rebuild_item();
DROP TABLE IF EXISTS commerce_project_rebuild_items;
DROP TABLE IF EXISTS commerce_product_rebuild_items;
DROP TABLE IF EXISTS commerce_product_rebuilds;

DROP TRIGGER IF EXISTS commerce_run_attempts_identity_immutable ON commerce_production_run_item_attempts;
DROP FUNCTION IF EXISTS protect_commerce_run_attempt_identity();
DROP INDEX IF EXISTS commerce_run_attempts_provider_idx;
DROP TABLE IF EXISTS commerce_production_run_item_attempts;

DROP TRIGGER IF EXISTS commerce_run_items_typed_subject ON commerce_production_run_items;
DROP FUNCTION IF EXISTS validate_commerce_run_item_subject();
DROP INDEX IF EXISTS commerce_run_items_status_idx;
DROP TABLE IF EXISTS commerce_production_run_items;

DROP INDEX IF EXISTS commerce_production_runs_unit_status_idx;
ALTER TABLE commerce_script_unit_batch_items
    DROP CONSTRAINT IF EXISTS commerce_batch_items_child_run_fk;
DROP TABLE IF EXISTS commerce_production_runs;
DROP INDEX IF EXISTS commerce_batch_items_status_idx;
DROP INDEX IF EXISTS commerce_batch_coordinators_retry_idx;
DROP INDEX IF EXISTS commerce_batch_coordinators_project_status_idx;
DROP TABLE IF EXISTS commerce_script_unit_batch_items;
DROP TABLE IF EXISTS commerce_script_unit_batch_coordinators;
