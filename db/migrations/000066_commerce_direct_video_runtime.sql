-- +goose Up

SET search_path TO public;

ALTER TABLE commerce_script_units
    DROP CONSTRAINT commerce_script_units_duration_check,
    ADD CONSTRAINT commerce_script_units_duration_check CHECK (target_duration_seconds > 0);

CREATE TABLE commerce_script_reference_images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    artifact_id UUID NOT NULL,
    media_file_id UUID NOT NULL,
    original_file_name TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    byte_size BIGINT NOT NULL,
    content_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    revision BIGINT NOT NULL DEFAULT 1,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ,
    CONSTRAINT commerce_script_reference_images_unit_fk
        FOREIGN KEY (script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_units(id, product_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_reference_images_artifact_fk
        FOREIGN KEY (artifact_id, organization_id, project_id)
        REFERENCES artifacts(id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_reference_images_media_fk
        FOREIGN KEY (media_file_id, organization_id, project_id)
        REFERENCES media_files(id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_reference_images_name_check CHECK (btrim(original_file_name) <> ''),
    CONSTRAINT commerce_script_reference_images_mime_check CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/webp')),
    CONSTRAINT commerce_script_reference_images_dimensions_check CHECK (width > 0 AND height > 0),
    CONSTRAINT commerce_script_reference_images_size_check CHECK (byte_size > 0),
    CONSTRAINT commerce_script_reference_images_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_script_reference_images_status_check CHECK (status IN ('active', 'archived')),
    CONSTRAINT commerce_script_reference_images_revision_check CHECK (revision > 0),
    CONSTRAINT commerce_script_reference_images_archive_check CHECK (
        (status = 'active' AND archived_at IS NULL)
        OR (status = 'archived' AND archived_at IS NOT NULL)
    ),
    UNIQUE(id, script_unit_id, organization_id, project_id)
);

CREATE UNIQUE INDEX commerce_script_reference_images_active_hash_uidx
    ON commerce_script_reference_images(script_unit_id, content_hash)
    WHERE status = 'active';

CREATE INDEX commerce_script_reference_images_list_idx
    ON commerce_script_reference_images(script_unit_id, status, created_at, id);

CREATE TABLE commerce_script_reference_uploads (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    storage_key TEXT NOT NULL UNIQUE,
    requested_mime_type TEXT NOT NULL,
    original_file_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    idempotency_key TEXT NOT NULL,
    reference_image_id UUID,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    abandoned_at TIMESTAMPTZ,
    CONSTRAINT commerce_script_reference_uploads_unit_fk
        FOREIGN KEY (script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_units(id, product_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_reference_uploads_reference_fk
        FOREIGN KEY (reference_image_id, script_unit_id, organization_id, project_id)
        REFERENCES commerce_script_reference_images(id, script_unit_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_reference_uploads_name_check CHECK (btrim(original_file_name) <> ''),
    CONSTRAINT commerce_script_reference_uploads_mime_check
        CHECK (requested_mime_type IN ('image/jpeg', 'image/png', 'image/webp')),
    CONSTRAINT commerce_script_reference_uploads_status_check
        CHECK (status IN ('pending', 'completed', 'abandoned', 'expired')),
    CONSTRAINT commerce_script_reference_uploads_lifecycle_check CHECK (
        (status = 'pending' AND reference_image_id IS NULL AND completed_at IS NULL AND abandoned_at IS NULL)
        OR (status = 'completed' AND reference_image_id IS NOT NULL AND completed_at IS NOT NULL AND abandoned_at IS NULL)
        OR (status IN ('abandoned', 'expired') AND reference_image_id IS NULL AND completed_at IS NULL AND abandoned_at IS NOT NULL)
    ),
    UNIQUE(organization_id, idempotency_key)
);

CREATE INDEX commerce_script_reference_uploads_cleanup_idx
    ON commerce_script_reference_uploads(status, expires_at)
    WHERE status = 'pending';

CREATE TABLE commerce_direct_video_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    product_version_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    script_unit_revision BIGINT NOT NULL,
    project_production_generation_id UUID NOT NULL,
    video_production_binding_id UUID NOT NULL,
    video_production_binding_revision BIGINT NOT NULL,
    video_profile_version_id UUID NOT NULL,
    video_profile_snapshot_hash TEXT NOT NULL,
    model_profile_key TEXT NOT NULL,
    model_profile_id UUID,
    model_profile_binding_id UUID,
    provider_model_id UUID,
    provider_account_id UUID,
    provider_model_key TEXT NOT NULL,
    route_key TEXT NOT NULL,
    variant_key TEXT NOT NULL,
    capability_snapshot_hash TEXT NOT NULL,
    requested_duration_seconds INTEGER NOT NULL,
    aspect_ratio TEXT NOT NULL,
    resolution TEXT NOT NULL,
    generate_audio BOOLEAN NOT NULL DEFAULT true,
    script_snapshot TEXT NOT NULL,
    script_hash TEXT NOT NULL,
    product_snapshot JSONB NOT NULL,
    product_snapshot_hash TEXT NOT NULL,
    execution_contract JSONB NOT NULL,
    execution_contract_hash TEXT NOT NULL,
    reference_set_hash TEXT NOT NULL,
    prompt_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    attempt_generation INTEGER NOT NULL DEFAULT 1,
    idempotency_key TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    node_run_id UUID REFERENCES workflow_node_runs(id) ON DELETE SET NULL,
    provider_request_id UUID REFERENCES provider_requests(id) ON DELETE SET NULL,
    provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    provider_async_task_id UUID REFERENCES provider_async_tasks(id) ON DELETE SET NULL,
    external_task_id TEXT,
    output_artifact_id UUID,
    output_media_file_id UUID,
    output_storage_key TEXT,
    output_mime_type TEXT,
    error_code TEXT,
    error_message TEXT,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_direct_video_jobs_unit_fk
        FOREIGN KEY (script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_units(id, product_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_direct_video_jobs_product_version_fk
        FOREIGN KEY (product_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_versions(id, product_id, organization_id, project_id) ON DELETE NO ACTION
        DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT commerce_direct_video_jobs_project_generation_fk
        FOREIGN KEY (project_production_generation_id, project_id, organization_id)
        REFERENCES project_video_production_generations(id, project_id, organization_id) ON DELETE NO ACTION
        DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT commerce_direct_video_jobs_video_binding_fk
        FOREIGN KEY (video_production_binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id) ON DELETE NO ACTION
        DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT commerce_direct_video_jobs_video_profile_version_fk
        FOREIGN KEY (video_profile_version_id)
        REFERENCES video_production_profile_versions(id) ON DELETE NO ACTION
        DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT commerce_direct_video_jobs_model_profile_fk
        FOREIGN KEY (model_profile_id)
        REFERENCES model_profiles(id) ON DELETE SET NULL,
    CONSTRAINT commerce_direct_video_jobs_model_profile_binding_fk
        FOREIGN KEY (model_profile_binding_id)
        REFERENCES model_profile_bindings(id) ON DELETE SET NULL,
    CONSTRAINT commerce_direct_video_jobs_provider_model_fk
        FOREIGN KEY (provider_model_id)
        REFERENCES provider_models(id) ON DELETE SET NULL,
    CONSTRAINT commerce_direct_video_jobs_provider_account_fk
        FOREIGN KEY (provider_account_id)
        REFERENCES provider_accounts(id) ON DELETE SET NULL,
    CONSTRAINT commerce_direct_video_jobs_output_artifact_fk
        FOREIGN KEY (output_artifact_id, organization_id, project_id)
        REFERENCES artifacts(id, organization_id, project_id)
        ON DELETE NO ACTION DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT commerce_direct_video_jobs_output_media_fk
        FOREIGN KEY (output_media_file_id, organization_id, project_id)
        REFERENCES media_files(id, organization_id, project_id)
        ON DELETE NO ACTION DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT commerce_direct_video_jobs_script_revision_check CHECK (script_unit_revision > 0),
    CONSTRAINT commerce_direct_video_jobs_binding_revision_check CHECK (video_production_binding_revision > 0),
    CONSTRAINT commerce_direct_video_jobs_duration_check CHECK (requested_duration_seconds > 0),
    CONSTRAINT commerce_direct_video_jobs_identity_text_check CHECK (
        btrim(model_profile_key) <> ''
        AND btrim(provider_model_key) <> ''
        AND btrim(route_key) <> ''
        AND btrim(variant_key) <> ''
        AND btrim(aspect_ratio) <> ''
        AND btrim(resolution) <> ''
        AND btrim(script_snapshot) <> ''
    ),
    CONSTRAINT commerce_direct_video_jobs_hashes_check CHECK (
        video_profile_snapshot_hash ~ '^[0-9a-f]{64}$'
        AND capability_snapshot_hash ~ '^[0-9a-f]{64}$'
        AND script_hash ~ '^[0-9a-f]{64}$'
        AND product_snapshot_hash ~ '^[0-9a-f]{64}$'
        AND execution_contract_hash ~ '^[0-9a-f]{64}$'
        AND reference_set_hash ~ '^[0-9a-f]{64}$'
        AND prompt_hash ~ '^[0-9a-f]{64}$'
        AND payload_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT commerce_direct_video_jobs_json_check CHECK (
        jsonb_typeof(product_snapshot) = 'object'
        AND jsonb_typeof(execution_contract) = 'object'
    ),
    CONSTRAINT commerce_direct_video_jobs_status_check CHECK (
        status IN ('queued', 'running', 'succeeded', 'failed', 'cancelling', 'cancelled')
    ),
    CONSTRAINT commerce_direct_video_jobs_attempt_check CHECK (attempt_generation > 0),
    CONSTRAINT commerce_direct_video_jobs_idempotency_check CHECK (btrim(idempotency_key) <> ''),
    CONSTRAINT commerce_direct_video_jobs_failure_check CHECK (
        (status = 'failed' AND error_code IS NOT NULL)
        OR status <> 'failed'
    ),
    CONSTRAINT commerce_direct_video_jobs_output_check CHECK (
        (status = 'succeeded' AND output_artifact_id IS NOT NULL AND output_media_file_id IS NOT NULL)
        OR status <> 'succeeded'
    ),
    CONSTRAINT commerce_direct_video_jobs_terminal_check CHECK (
        (status IN ('succeeded', 'failed', 'cancelled') AND completed_at IS NOT NULL)
        OR (status NOT IN ('succeeded', 'failed', 'cancelled') AND completed_at IS NULL)
    ),
    UNIQUE(organization_id, idempotency_key),
    UNIQUE(id, organization_id, project_id),
    UNIQUE(id, product_id, script_unit_id, organization_id, project_id)
);

CREATE INDEX commerce_direct_video_jobs_script_idx
    ON commerce_direct_video_jobs(script_unit_id, created_at DESC, id DESC);

CREATE INDEX commerce_direct_video_jobs_project_status_idx
    ON commerce_direct_video_jobs(project_id, status, created_at DESC);

CREATE INDEX commerce_direct_video_jobs_active_idx
    ON commerce_direct_video_jobs(status, updated_at)
    WHERE status IN ('queued', 'running', 'cancelling');

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_direct_video_job_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    actual_binding_revision BIGINT;
    actual_profile_version_id UUID;
    actual_profile_snapshot_hash TEXT;
    actual_generation_binding_id UUID;
    actual_generation_status TEXT;
    actual_profile_key TEXT;
    actual_provider_model_id UUID;
    actual_provider_account_id UUID;
    actual_provider_model_key TEXT;
BEGIN
    IF NEW.model_profile_id IS NULL
       OR NEW.model_profile_binding_id IS NULL
       OR NEW.provider_model_id IS NULL
       OR NEW.provider_account_id IS NULL THEN
        RAISE EXCEPTION 'commerce direct video route identity is incomplete' USING ERRCODE = '23514';
    END IF;

    SELECT binding.revision, binding.profile_version_id, binding.profile_snapshot_hash,
           generation.binding_id, generation.status
    INTO actual_binding_revision, actual_profile_version_id, actual_profile_snapshot_hash,
         actual_generation_binding_id, actual_generation_status
    FROM project_video_production_generations generation
    JOIN project_video_production_bindings binding
      ON binding.id = generation.binding_id
     AND binding.project_id = generation.project_id
    WHERE generation.id = NEW.project_production_generation_id
      AND generation.project_id = NEW.project_id
      AND generation.organization_id = NEW.organization_id;

    IF NOT FOUND
       OR actual_generation_status <> 'active'
       OR actual_generation_binding_id <> NEW.video_production_binding_id
       OR actual_binding_revision <> NEW.video_production_binding_revision
       OR actual_profile_version_id <> NEW.video_profile_version_id
       OR actual_profile_snapshot_hash <> NEW.video_profile_snapshot_hash THEN
        RAISE EXCEPTION 'commerce direct video production identity does not match active generation'
            USING ERRCODE = '23514';
    END IF;

    SELECT profile.profile_key, binding.provider_model_id,
           model.provider_account_id, model.model_key
    INTO actual_profile_key, actual_provider_model_id,
         actual_provider_account_id, actual_provider_model_key
    FROM model_profile_bindings binding
    JOIN model_profiles profile ON profile.id = binding.model_profile_id
    JOIN provider_models model ON model.id = binding.provider_model_id
    JOIN provider_accounts account ON account.id = model.provider_account_id
    WHERE binding.id = NEW.model_profile_binding_id
      AND binding.model_profile_id = NEW.model_profile_id
      AND profile.organization_id = NEW.organization_id
      AND binding.enabled
      AND model.status = 'active'
      AND account.status = 'active';

    IF NOT FOUND
       OR actual_profile_key <> NEW.model_profile_key
       OR actual_provider_model_id <> NEW.provider_model_id
       OR actual_provider_account_id <> NEW.provider_account_id
       OR actual_provider_model_key <> NEW.provider_model_key THEN
        RAISE EXCEPTION 'commerce direct video provider route is not active or does not match its snapshot'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_direct_video_jobs_validate_identity
BEFORE INSERT ON commerce_direct_video_jobs
FOR EACH ROW EXECUTE FUNCTION validate_commerce_direct_video_job_identity();

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_direct_video_job()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.organization_id IS DISTINCT FROM OLD.organization_id
       OR NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.product_id IS DISTINCT FROM OLD.product_id
       OR NEW.product_version_id IS DISTINCT FROM OLD.product_version_id
       OR NEW.script_unit_id IS DISTINCT FROM OLD.script_unit_id
       OR NEW.script_unit_revision IS DISTINCT FROM OLD.script_unit_revision
       OR NEW.project_production_generation_id IS DISTINCT FROM OLD.project_production_generation_id
       OR NEW.video_production_binding_id IS DISTINCT FROM OLD.video_production_binding_id
       OR NEW.video_production_binding_revision IS DISTINCT FROM OLD.video_production_binding_revision
       OR NEW.video_profile_version_id IS DISTINCT FROM OLD.video_profile_version_id
       OR NEW.video_profile_snapshot_hash IS DISTINCT FROM OLD.video_profile_snapshot_hash
       OR NEW.model_profile_key IS DISTINCT FROM OLD.model_profile_key
       OR NEW.provider_model_key IS DISTINCT FROM OLD.provider_model_key
       OR NEW.route_key IS DISTINCT FROM OLD.route_key
       OR NEW.variant_key IS DISTINCT FROM OLD.variant_key
       OR NEW.capability_snapshot_hash IS DISTINCT FROM OLD.capability_snapshot_hash
       OR NEW.requested_duration_seconds IS DISTINCT FROM OLD.requested_duration_seconds
       OR NEW.aspect_ratio IS DISTINCT FROM OLD.aspect_ratio
       OR NEW.resolution IS DISTINCT FROM OLD.resolution
       OR NEW.generate_audio IS DISTINCT FROM OLD.generate_audio
       OR NEW.script_snapshot IS DISTINCT FROM OLD.script_snapshot
       OR NEW.script_hash IS DISTINCT FROM OLD.script_hash
       OR NEW.product_snapshot IS DISTINCT FROM OLD.product_snapshot
       OR NEW.product_snapshot_hash IS DISTINCT FROM OLD.product_snapshot_hash
       OR NEW.execution_contract IS DISTINCT FROM OLD.execution_contract
       OR NEW.execution_contract_hash IS DISTINCT FROM OLD.execution_contract_hash
       OR NEW.reference_set_hash IS DISTINCT FROM OLD.reference_set_hash
       OR NEW.prompt_hash IS DISTINCT FROM OLD.prompt_hash
       OR NEW.attempt_generation IS DISTINCT FROM OLD.attempt_generation
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.payload_hash IS DISTINCT FROM OLD.payload_hash
       OR NEW.workflow_run_id IS DISTINCT FROM OLD.workflow_run_id
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'commerce direct video job snapshots are immutable' USING ERRCODE = '55000';
    END IF;

    IF NEW.model_profile_id IS DISTINCT FROM OLD.model_profile_id
       AND NOT (OLD.model_profile_id IS NOT NULL AND NEW.model_profile_id IS NULL) THEN
        RAISE EXCEPTION 'commerce direct video model profile identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.model_profile_binding_id IS DISTINCT FROM OLD.model_profile_binding_id
       AND NOT (OLD.model_profile_binding_id IS NOT NULL AND NEW.model_profile_binding_id IS NULL) THEN
        RAISE EXCEPTION 'commerce direct video model binding identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.provider_model_id IS DISTINCT FROM OLD.provider_model_id
       AND NOT (OLD.provider_model_id IS NOT NULL AND NEW.provider_model_id IS NULL) THEN
        RAISE EXCEPTION 'commerce direct video provider model identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.provider_account_id IS DISTINCT FROM OLD.provider_account_id
       AND NOT (OLD.provider_account_id IS NOT NULL AND NEW.provider_account_id IS NULL) THEN
        RAISE EXCEPTION 'commerce direct video provider account identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.node_run_id IS DISTINCT FROM OLD.node_run_id
       AND NOT (OLD.node_run_id IS NULL AND NEW.node_run_id IS NOT NULL) THEN
        RAISE EXCEPTION 'commerce direct video node identity cannot be replaced' USING ERRCODE = '55000';
    END IF;
    IF NEW.provider_request_id IS DISTINCT FROM OLD.provider_request_id
       AND NOT (OLD.provider_request_id IS NULL AND NEW.provider_request_id IS NOT NULL) THEN
        RAISE EXCEPTION 'commerce direct video provider request cannot be replaced' USING ERRCODE = '55000';
    END IF;
    IF NEW.provider_call_id IS DISTINCT FROM OLD.provider_call_id
       AND NOT (OLD.provider_call_id IS NULL AND NEW.provider_call_id IS NOT NULL) THEN
        RAISE EXCEPTION 'commerce direct video provider call cannot be replaced' USING ERRCODE = '55000';
    END IF;
    IF NEW.provider_async_task_id IS DISTINCT FROM OLD.provider_async_task_id
       AND NOT (OLD.provider_async_task_id IS NULL AND NEW.provider_async_task_id IS NOT NULL) THEN
        RAISE EXCEPTION 'commerce direct video provider task cannot be replaced' USING ERRCODE = '55000';
    END IF;
    IF NEW.external_task_id IS DISTINCT FROM OLD.external_task_id
       AND NOT (OLD.external_task_id IS NULL AND NEW.external_task_id IS NOT NULL) THEN
        RAISE EXCEPTION 'commerce direct video external task cannot be replaced' USING ERRCODE = '55000';
    END IF;
    IF NEW.output_artifact_id IS DISTINCT FROM OLD.output_artifact_id
       AND NOT (OLD.output_artifact_id IS NULL AND NEW.output_artifact_id IS NOT NULL) THEN
        RAISE EXCEPTION 'commerce direct video output artifact cannot be replaced' USING ERRCODE = '55000';
    END IF;
    IF NEW.output_media_file_id IS DISTINCT FROM OLD.output_media_file_id
       AND NOT (OLD.output_media_file_id IS NULL AND NEW.output_media_file_id IS NOT NULL) THEN
        RAISE EXCEPTION 'commerce direct video output media cannot be replaced' USING ERRCODE = '55000';
    END IF;
    IF NEW.output_storage_key IS DISTINCT FROM OLD.output_storage_key
       AND NOT (OLD.output_storage_key IS NULL AND NEW.output_storage_key IS NOT NULL) THEN
        RAISE EXCEPTION 'commerce direct video output storage identity cannot be replaced' USING ERRCODE = '55000';
    END IF;
    IF NEW.output_mime_type IS DISTINCT FROM OLD.output_mime_type
       AND NOT (OLD.output_mime_type IS NULL AND NEW.output_mime_type IS NOT NULL) THEN
        RAISE EXCEPTION 'commerce direct video output mime type cannot be replaced' USING ERRCODE = '55000';
    END IF;
    IF NEW.error_code IS DISTINCT FROM OLD.error_code
       AND NOT (OLD.error_code IS NULL AND NEW.error_code IS NOT NULL) THEN
        RAISE EXCEPTION 'commerce direct video terminal error code cannot be replaced' USING ERRCODE = '55000';
    END IF;
    IF NEW.error_message IS DISTINCT FROM OLD.error_message
       AND NOT (OLD.error_message IS NULL AND NEW.error_message IS NOT NULL) THEN
        RAISE EXCEPTION 'commerce direct video terminal error message cannot be replaced' USING ERRCODE = '55000';
    END IF;
    IF NEW.started_at IS DISTINCT FROM OLD.started_at
       AND NOT (OLD.started_at IS NULL AND NEW.started_at IS NOT NULL) THEN
        RAISE EXCEPTION 'commerce direct video start time cannot be replaced' USING ERRCODE = '55000';
    END IF;
    IF NEW.completed_at IS DISTINCT FROM OLD.completed_at
       AND NOT (OLD.completed_at IS NULL AND NEW.completed_at IS NOT NULL) THEN
        RAISE EXCEPTION 'commerce direct video completion time cannot be replaced' USING ERRCODE = '55000';
    END IF;
    IF NEW.cancelled_at IS DISTINCT FROM OLD.cancelled_at
       AND NOT (OLD.cancelled_at IS NULL AND NEW.cancelled_at IS NOT NULL) THEN
        RAISE EXCEPTION 'commerce direct video cancellation time cannot be replaced' USING ERRCODE = '55000';
    END IF;
    IF (NEW.status = 'queued' AND OLD.status <> 'queued')
       OR (NEW.status = 'running' AND OLD.status NOT IN ('queued', 'running'))
       OR (NEW.status = 'cancelling' AND OLD.status NOT IN ('queued', 'running', 'cancelling'))
       OR (NEW.status = 'succeeded' AND OLD.status NOT IN ('running', 'succeeded'))
       OR (NEW.status = 'failed' AND OLD.status NOT IN ('queued', 'running', 'cancelling', 'failed'))
       OR (NEW.status = 'cancelled' AND OLD.status NOT IN ('queued', 'running', 'cancelling', 'cancelled')) THEN
        RAISE EXCEPTION 'invalid commerce direct video job status transition' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_direct_video_jobs_protect_snapshot
BEFORE UPDATE ON commerce_direct_video_jobs
FOR EACH ROW EXECUTE FUNCTION protect_commerce_direct_video_job();

CREATE TABLE commerce_direct_video_job_references (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    job_id UUID NOT NULL,
    source_type TEXT NOT NULL,
    source_id UUID NOT NULL,
    product_reference_id UUID,
    script_reference_image_id UUID,
    artifact_id UUID NOT NULL,
    media_file_id UUID NOT NULL,
    reference_role TEXT NOT NULL,
    ordinal INTEGER NOT NULL,
    content_hash TEXT NOT NULL,
    source_revision BIGINT NOT NULL,
    snapshot JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_direct_video_job_refs_job_fk
        FOREIGN KEY (job_id, product_id, script_unit_id, organization_id, project_id)
        REFERENCES commerce_direct_video_jobs(id, product_id, script_unit_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_direct_video_job_refs_product_fk
        FOREIGN KEY (product_reference_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_references(id, product_id, organization_id, project_id)
        ON DELETE NO ACTION DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT commerce_direct_video_job_refs_script_fk
        FOREIGN KEY (script_reference_image_id, script_unit_id, organization_id, project_id)
        REFERENCES commerce_script_reference_images(id, script_unit_id, organization_id, project_id)
        ON DELETE NO ACTION DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT commerce_direct_video_job_refs_artifact_fk
        FOREIGN KEY (artifact_id, organization_id, project_id)
        REFERENCES artifacts(id, organization_id, project_id) ON DELETE NO ACTION DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT commerce_direct_video_job_refs_media_fk
        FOREIGN KEY (media_file_id, organization_id, project_id)
        REFERENCES media_files(id, organization_id, project_id) ON DELETE NO ACTION DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT commerce_direct_video_job_refs_source_check CHECK (
        (source_type = 'product' AND product_reference_id = source_id AND script_reference_image_id IS NULL)
        OR (source_type = 'custom' AND script_reference_image_id = source_id AND product_reference_id IS NULL)
    ),
    CONSTRAINT commerce_direct_video_job_refs_role_check CHECK (
        reference_role IN ('first_frame', 'semantic_reference')
    ),
    CONSTRAINT commerce_direct_video_job_refs_ordinal_check CHECK (ordinal >= 0),
    CONSTRAINT commerce_direct_video_job_refs_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_direct_video_job_refs_revision_check CHECK (source_revision > 0),
    CONSTRAINT commerce_direct_video_job_refs_snapshot_check CHECK (jsonb_typeof(snapshot) = 'object'),
    UNIQUE(job_id, ordinal),
    UNIQUE(job_id, source_type, source_id)
);

CREATE INDEX commerce_direct_video_job_refs_source_idx
    ON commerce_direct_video_job_references(source_type, source_id, created_at DESC);

-- +goose Down

SET search_path TO public;

DROP TABLE IF EXISTS commerce_direct_video_job_references;
DROP TRIGGER IF EXISTS commerce_direct_video_jobs_protect_snapshot ON commerce_direct_video_jobs;
DROP FUNCTION IF EXISTS protect_commerce_direct_video_job();
DROP TRIGGER IF EXISTS commerce_direct_video_jobs_validate_identity ON commerce_direct_video_jobs;
DROP FUNCTION IF EXISTS validate_commerce_direct_video_job_identity();
DROP TABLE IF EXISTS commerce_direct_video_jobs;
DROP TABLE IF EXISTS commerce_script_reference_uploads;
DROP TABLE IF EXISTS commerce_script_reference_images;

ALTER TABLE commerce_script_units
    DROP CONSTRAINT commerce_script_units_duration_check,
    ADD CONSTRAINT commerce_script_units_duration_check
        CHECK (target_duration_seconds IN (15, 30, 60));
