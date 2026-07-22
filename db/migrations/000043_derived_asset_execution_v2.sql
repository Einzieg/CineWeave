-- +goose Up

SET search_path TO public;

CREATE TABLE derived_asset_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow_run_id UUID NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    production_generation_id UUID NOT NULL,
    video_production_binding_id UUID NOT NULL,
    video_production_binding_revision BIGINT NOT NULL,
    root_batch_id UUID REFERENCES derived_asset_batches(id) ON DELETE RESTRICT,
    retry_of_batch_id UUID REFERENCES derived_asset_batches(id) ON DELETE RESTRICT,
    retry_depth INTEGER NOT NULL DEFAULT 0,
    request_mode TEXT NOT NULL,
    filters JSONB NOT NULL DEFAULT '{}'::jsonb,
    filters_hash TEXT NOT NULL,
    selector_candidate_count INTEGER NOT NULL DEFAULT 0,
    selector_skipped_count INTEGER NOT NULL DEFAULT 0,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'prepared',
    revision BIGINT NOT NULL DEFAULT 1,
    total_items INTEGER NOT NULL DEFAULT 0,
    executable_items INTEGER NOT NULL DEFAULT 0,
    review_required_items INTEGER NOT NULL DEFAULT 0,
    not_found_items INTEGER NOT NULL DEFAULT 0,
    generation_mismatch_items INTEGER NOT NULL DEFAULT 0,
    already_running_items INTEGER NOT NULL DEFAULT 0,
    duplicate_items INTEGER NOT NULL DEFAULT 0,
    skipped_items INTEGER NOT NULL DEFAULT 0,
    pending_items INTEGER NOT NULL DEFAULT 0,
    queued_items INTEGER NOT NULL DEFAULT 0,
    running_items INTEGER NOT NULL DEFAULT 0,
    succeeded_items INTEGER NOT NULL DEFAULT 0,
    failed_retryable_items INTEGER NOT NULL DEFAULT 0,
    failed_terminal_items INTEGER NOT NULL DEFAULT 0,
    cancelled_items INTEGER NOT NULL DEFAULT 0,
    discarded_items INTEGER NOT NULL DEFAULT 0,
    error_code TEXT,
    error_message TEXT,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT derived_asset_batches_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT derived_asset_batches_binding_fk
        FOREIGN KEY (video_production_binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT derived_asset_batches_binding_revision_check CHECK (video_production_binding_revision > 0),
    CONSTRAINT derived_asset_batches_retry_depth_check CHECK (retry_depth >= 0),
    CONSTRAINT derived_asset_batches_request_mode_check CHECK (
        request_mode IN ('explicit', 'select_all', 'retry')
    ),
    CONSTRAINT derived_asset_batches_retry_shape_check CHECK (
        (
            request_mode <> 'retry'
            AND root_batch_id IS NULL
            AND retry_of_batch_id IS NULL
            AND retry_depth = 0
        ) OR (
            request_mode = 'retry'
            AND root_batch_id IS NOT NULL
            AND retry_of_batch_id IS NOT NULL
            AND retry_depth > 0
            AND root_batch_id <> id
            AND retry_of_batch_id <> id
        )
    ),
    CONSTRAINT derived_asset_batches_filters_check CHECK (
        jsonb_typeof(filters) = 'object'
    ),
    CONSTRAINT derived_asset_batches_selection_hash_check CHECK (
        filters_hash ~ '^(sha256:)?[0-9a-f]{64}$'
    ),
    CONSTRAINT derived_asset_batches_selector_counts_check CHECK (
        selector_candidate_count >= 0 AND selector_skipped_count >= 0
    ),
    CONSTRAINT derived_asset_batches_idempotency_key_check CHECK (btrim(idempotency_key) <> ''),
    CONSTRAINT derived_asset_batches_request_hash_check CHECK (
        request_hash ~ '^(sha256:)?[0-9a-f]{64}$'
    ),
    CONSTRAINT derived_asset_batches_status_check CHECK (
        status IN (
            'prepared', 'queued', 'running', 'succeeded', 'partial_succeeded',
            'failed', 'cancelled', 'discarded'
        )
    ),
    CONSTRAINT derived_asset_batches_revision_check CHECK (revision > 0),
    CONSTRAINT derived_asset_batches_counts_nonnegative_check CHECK (
        total_items >= 0
        AND executable_items >= 0
        AND review_required_items >= 0
        AND not_found_items >= 0
        AND generation_mismatch_items >= 0
        AND already_running_items >= 0
        AND duplicate_items >= 0
        AND skipped_items >= 0
        AND pending_items >= 0
        AND queued_items >= 0
        AND running_items >= 0
        AND succeeded_items >= 0
        AND failed_retryable_items >= 0
        AND failed_terminal_items >= 0
        AND cancelled_items >= 0
        AND discarded_items >= 0
    ),
    CONSTRAINT derived_asset_batches_disposition_aggregate_check CHECK (
        total_items = executable_items
            + review_required_items
            + not_found_items
            + generation_mismatch_items
            + already_running_items
            + duplicate_items
            + skipped_items
    ),
    CONSTRAINT derived_asset_batches_execution_aggregate_check CHECK (
        executable_items = pending_items
            + queued_items
            + running_items
            + succeeded_items
            + failed_retryable_items
            + failed_terminal_items
            + cancelled_items
            + discarded_items
    ),
    CONSTRAINT derived_asset_batches_terminal_time_check CHECK (
        (
            status IN ('succeeded', 'partial_succeeded', 'failed', 'cancelled', 'discarded')
            AND completed_at IS NOT NULL
            AND pending_items = 0
            AND queued_items = 0
            AND running_items = 0
        ) OR (
            status IN ('prepared', 'queued', 'running')
            AND completed_at IS NULL
        )
    ),
    CONSTRAINT derived_asset_batches_success_shape_check CHECK (
        status <> 'succeeded'
        OR (
            total_items > 0
            AND executable_items > 0
            AND succeeded_items = executable_items
            AND review_required_items = 0
            AND not_found_items = 0
            AND generation_mismatch_items = 0
            AND already_running_items = 0
            AND duplicate_items = 0
            AND skipped_items = 0
        )
    ),
    CONSTRAINT derived_asset_batches_partial_shape_check CHECK (
        status <> 'partial_succeeded'
        OR (
            succeeded_items > 0
            AND succeeded_items < total_items
        )
    ),
    CONSTRAINT derived_asset_batches_failed_shape_check CHECK (
        status <> 'failed'
        OR (
            total_items > 0
            AND succeeded_items = 0
            AND error_code IS NOT NULL
            AND btrim(error_code) <> ''
        )
    ),
    CONSTRAINT derived_asset_batches_identity_unique UNIQUE (id, organization_id, project_id)
);

CREATE UNIQUE INDEX derived_asset_batches_idempotency_uidx
    ON derived_asset_batches(organization_id, project_id, idempotency_key);

CREATE INDEX derived_asset_batches_project_status_idx
    ON derived_asset_batches(project_id, production_generation_id, status, created_at DESC);

CREATE INDEX derived_asset_batches_retry_idx
    ON derived_asset_batches(root_batch_id, retry_depth, created_at)
    WHERE root_batch_id IS NOT NULL;

CREATE INDEX derived_asset_batches_stuck_scanner_idx
    ON derived_asset_batches(updated_at, id)
    WHERE status IN ('prepared', 'queued', 'running');

-- +goose StatementBegin
CREATE FUNCTION validate_derived_asset_batch_lineage()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    parent derived_asset_batches%ROWTYPE;
    expected_root UUID;
BEGIN
    IF NEW.retry_of_batch_id IS NULL THEN
        IF NEW.request_mode = 'retry' OR NEW.root_batch_id IS NOT NULL OR NEW.retry_depth <> 0 THEN
            RAISE EXCEPTION 'invalid root derived asset batch lineage' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    SELECT * INTO parent
    FROM derived_asset_batches
    WHERE id = NEW.retry_of_batch_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'retry source derived asset batch does not exist' USING ERRCODE = '23503';
    END IF;

    expected_root := COALESCE(parent.root_batch_id, parent.id);
    IF NEW.request_mode <> 'retry'
       OR NEW.root_batch_id IS DISTINCT FROM expected_root
       OR NEW.retry_depth <> parent.retry_depth + 1
       OR NEW.organization_id <> parent.organization_id
       OR NEW.project_id <> parent.project_id
       OR NEW.production_generation_id <> parent.production_generation_id
       OR NEW.video_production_binding_id <> parent.video_production_binding_id
       OR NEW.video_production_binding_revision <> parent.video_production_binding_revision THEN
        RAISE EXCEPTION 'derived asset retry batch identity does not match its parent' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER derived_asset_batches_validate_lineage
BEFORE INSERT ON derived_asset_batches
FOR EACH ROW EXECUTE FUNCTION validate_derived_asset_batch_lineage();

-- +goose StatementBegin
CREATE FUNCTION protect_derived_asset_batch_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF ROW(
        NEW.organization_id,
        NEW.project_id,
        NEW.workflow_run_id,
        NEW.production_generation_id,
        NEW.video_production_binding_id,
        NEW.video_production_binding_revision,
        NEW.root_batch_id,
        NEW.retry_of_batch_id,
        NEW.retry_depth,
        NEW.request_mode,
        NEW.filters,
        NEW.filters_hash,
        NEW.selector_candidate_count,
        NEW.selector_skipped_count,
        NEW.idempotency_key,
        NEW.request_hash,
        NEW.created_by,
        NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.organization_id,
        OLD.project_id,
        OLD.workflow_run_id,
        OLD.production_generation_id,
        OLD.video_production_binding_id,
        OLD.video_production_binding_revision,
        OLD.root_batch_id,
        OLD.retry_of_batch_id,
        OLD.retry_depth,
        OLD.request_mode,
        OLD.filters,
        OLD.filters_hash,
        OLD.selector_candidate_count,
        OLD.selector_skipped_count,
        OLD.idempotency_key,
        OLD.request_hash,
        OLD.created_by,
        OLD.created_at
    ) THEN
        RAISE EXCEPTION 'derived asset batch request identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status IN ('succeeded', 'partial_succeeded', 'failed', 'cancelled', 'discarded')
       AND NEW.status IS DISTINCT FROM OLD.status THEN
        RAISE EXCEPTION 'terminal derived asset batch status is immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW IS DISTINCT FROM OLD AND NEW.revision <> OLD.revision + 1 THEN
        RAISE EXCEPTION 'derived asset batch update requires the next CAS revision' USING ERRCODE = '40001';
    END IF;
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER derived_asset_batches_protect_identity
BEFORE UPDATE ON derived_asset_batches
FOR EACH ROW EXECUTE FUNCTION protect_derived_asset_batch_identity();

CREATE TABLE derived_asset_request_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id UUID NOT NULL,
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    input_ordinal INTEGER NOT NULL,
    original_id TEXT NOT NULL,
    requirement_id UUID,
    duplicate_of_request_item_id UUID REFERENCES derived_asset_request_items(id) ON DELETE RESTRICT,
    root_request_item_id UUID REFERENCES derived_asset_request_items(id) ON DELETE RESTRICT,
    retry_of_request_item_id UUID REFERENCES derived_asset_request_items(id) ON DELETE RESTRICT,
    disposition TEXT NOT NULL,
    disposition_detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code TEXT,
    error_message TEXT,
    retryable BOOLEAN NOT NULL DEFAULT false,
    input_snapshot JSONB NOT NULL,
    input_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    current_attempt_id UUID,
    current_attempt_no INTEGER,
    revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT derived_asset_request_items_batch_fk
        FOREIGN KEY (batch_id, organization_id, project_id)
        REFERENCES derived_asset_batches(id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT derived_asset_request_items_ordinal_check CHECK (input_ordinal > 0),
    CONSTRAINT derived_asset_request_items_original_id_check CHECK (btrim(original_id) <> ''),
    CONSTRAINT derived_asset_request_items_duplicate_shape_check CHECK (
        (disposition = 'duplicate' AND duplicate_of_request_item_id IS NOT NULL)
        OR (disposition <> 'duplicate' AND duplicate_of_request_item_id IS NULL)
    ),
    CONSTRAINT derived_asset_request_items_retry_shape_check CHECK (
        (root_request_item_id IS NULL AND retry_of_request_item_id IS NULL)
        OR (
            root_request_item_id IS NOT NULL
            AND retry_of_request_item_id IS NOT NULL
            AND root_request_item_id <> id
            AND retry_of_request_item_id <> id
        )
    ),
    CONSTRAINT derived_asset_request_items_disposition_check CHECK (
        disposition IN (
            'executable', 'review_required', 'not_found', 'generation_mismatch',
            'already_running', 'duplicate', 'skipped'
        )
    ),
    CONSTRAINT derived_asset_request_items_disposition_detail_check CHECK (
        jsonb_typeof(disposition_detail) = 'object'
    ),
    CONSTRAINT derived_asset_request_items_input_snapshot_check CHECK (
        jsonb_typeof(input_snapshot) = 'object'
    ),
    CONSTRAINT derived_asset_request_items_input_hash_check CHECK (
        input_hash ~ '^(sha256:)?[0-9a-f]{64}$'
    ),
    CONSTRAINT derived_asset_request_items_status_check CHECK (
        status IN (
            'pending', 'queued', 'running', 'succeeded', 'failed_retryable',
            'failed_terminal', 'cancelled', 'discarded', 'blocked', 'skipped'
        )
    ),
    CONSTRAINT derived_asset_request_items_disposition_status_check CHECK (
        (
            disposition = 'executable'
            AND status IN (
                'pending', 'queued', 'running', 'succeeded', 'failed_retryable',
                'failed_terminal', 'cancelled', 'discarded'
            )
        ) OR (
            disposition IN ('review_required', 'not_found', 'generation_mismatch', 'already_running')
            AND status = 'blocked'
        ) OR (
            disposition IN ('duplicate', 'skipped')
            AND status = 'skipped'
        )
    ),
    CONSTRAINT derived_asset_request_items_retryable_check CHECK (
        NOT retryable OR disposition IN ('executable', 'review_required', 'already_running')
    ),
    CONSTRAINT derived_asset_request_items_error_shape_check CHECK (
        (
            disposition IN ('review_required', 'not_found', 'generation_mismatch', 'already_running')
            AND error_code IS NOT NULL
            AND btrim(error_code) <> ''
        ) OR disposition NOT IN ('review_required', 'not_found', 'generation_mismatch', 'already_running')
    ),
    CONSTRAINT derived_asset_request_items_failure_status_check CHECK (
        status NOT IN ('failed_retryable', 'failed_terminal')
        OR (error_code IS NOT NULL AND btrim(error_code) <> '')
    ),
    CONSTRAINT derived_asset_request_items_attempt_pointer_check CHECK (
        (current_attempt_id IS NULL) = (current_attempt_no IS NULL)
        AND (current_attempt_no IS NULL OR current_attempt_no > 0)
        AND (
            disposition <> 'executable'
            OR status = 'pending'
            OR current_attempt_id IS NOT NULL
        )
    ),
    CONSTRAINT derived_asset_request_items_revision_check CHECK (revision > 0),
    CONSTRAINT derived_asset_request_items_batch_ordinal_unique UNIQUE (batch_id, input_ordinal),
    CONSTRAINT derived_asset_request_items_identity_unique UNIQUE (
        id, batch_id, organization_id, project_id
    )
);

CREATE INDEX derived_asset_request_items_batch_disposition_idx
    ON derived_asset_request_items(batch_id, disposition, input_ordinal);

CREATE INDEX derived_asset_request_items_original_id_idx
    ON derived_asset_request_items(organization_id, project_id, original_id, created_at DESC);

CREATE INDEX derived_asset_request_items_retry_idx
    ON derived_asset_request_items(root_request_item_id, created_at)
    WHERE root_request_item_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION validate_derived_asset_request_item_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    owning_batch derived_asset_batches%ROWTYPE;
    duplicate_source derived_asset_request_items%ROWTYPE;
    retry_source derived_asset_request_items%ROWTYPE;
    retry_source_batch derived_asset_batches%ROWTYPE;
    expected_root UUID;
BEGIN
    SELECT * INTO owning_batch
    FROM derived_asset_batches
    WHERE id = NEW.batch_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'derived asset request item batch does not exist' USING ERRCODE = '23503';
    END IF;

    IF NEW.duplicate_of_request_item_id IS NOT NULL THEN
        SELECT * INTO duplicate_source
        FROM derived_asset_request_items
        WHERE id = NEW.duplicate_of_request_item_id;
        IF NOT FOUND
           OR duplicate_source.batch_id <> NEW.batch_id
           OR duplicate_source.input_ordinal >= NEW.input_ordinal
           OR duplicate_source.original_id <> NEW.original_id THEN
            RAISE EXCEPTION 'duplicate derived asset request item must reference an earlier identical input' USING ERRCODE = '23514';
        END IF;
    END IF;

    IF NEW.retry_of_request_item_id IS NULL THEN
        IF owning_batch.request_mode = 'retry' OR NEW.root_request_item_id IS NOT NULL THEN
            RAISE EXCEPTION 'retry batch request item must reference its source item' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    SELECT * INTO retry_source
    FROM derived_asset_request_items
    WHERE id = NEW.retry_of_request_item_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'retry source derived asset request item does not exist' USING ERRCODE = '23503';
    END IF;
    SELECT * INTO retry_source_batch
    FROM derived_asset_batches
    WHERE id = retry_source.batch_id;

    expected_root := COALESCE(retry_source.root_request_item_id, retry_source.id);
    IF owning_batch.request_mode <> 'retry'
       OR owning_batch.retry_of_batch_id <> retry_source.batch_id
       OR NEW.root_request_item_id IS DISTINCT FROM expected_root
       OR NEW.original_id <> retry_source.original_id
       OR NEW.input_hash <> retry_source.input_hash
       OR NEW.organization_id <> retry_source.organization_id
       OR NEW.project_id <> retry_source.project_id
       OR retry_source_batch.production_generation_id <> owning_batch.production_generation_id
       OR retry_source_batch.video_production_binding_id <> owning_batch.video_production_binding_id
       OR retry_source_batch.video_production_binding_revision <> owning_batch.video_production_binding_revision THEN
        RAISE EXCEPTION 'derived asset retry request item identity does not match its source' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER derived_asset_request_items_validate_identity
BEFORE INSERT ON derived_asset_request_items
FOR EACH ROW EXECUTE FUNCTION validate_derived_asset_request_item_identity();

-- +goose StatementBegin
CREATE FUNCTION protect_derived_asset_request_item_snapshot()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF ROW(
        NEW.batch_id,
        NEW.organization_id,
        NEW.project_id,
        NEW.input_ordinal,
        NEW.original_id,
        NEW.requirement_id,
        NEW.duplicate_of_request_item_id,
        NEW.root_request_item_id,
        NEW.retry_of_request_item_id,
        NEW.disposition,
        NEW.disposition_detail,
        NEW.retryable,
        NEW.input_snapshot,
        NEW.input_hash,
        NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.batch_id,
        OLD.organization_id,
        OLD.project_id,
        OLD.input_ordinal,
        OLD.original_id,
        OLD.requirement_id,
        OLD.duplicate_of_request_item_id,
        OLD.root_request_item_id,
        OLD.retry_of_request_item_id,
        OLD.disposition,
        OLD.disposition_detail,
        OLD.retryable,
        OLD.input_snapshot,
        OLD.input_hash,
        OLD.created_at
    ) THEN
        RAISE EXCEPTION 'derived asset request item snapshot is immutable' USING ERRCODE = '55000';
    END IF;
    IF ROW(NEW.error_code, NEW.error_message) IS DISTINCT FROM ROW(OLD.error_code, OLD.error_message)
       AND NOT (
        OLD.disposition = 'executable'
        AND OLD.status IN ('pending', 'queued', 'running')
        AND NEW.status IN ('failed_retryable', 'failed_terminal', 'cancelled', 'discarded')
       ) THEN
        RAISE EXCEPTION 'derived asset request item error outcome is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status IN (
        'succeeded', 'failed_retryable', 'failed_terminal', 'cancelled', 'discarded', 'blocked', 'skipped'
    ) AND NEW.status IS DISTINCT FROM OLD.status THEN
        RAISE EXCEPTION 'terminal derived asset request item outcome is immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW IS DISTINCT FROM OLD AND NEW.revision <> OLD.revision + 1 THEN
        RAISE EXCEPTION 'derived asset request item update requires the next CAS revision' USING ERRCODE = '40001';
    END IF;
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER derived_asset_request_items_protect_snapshot
BEFORE UPDATE ON derived_asset_request_items
FOR EACH ROW EXECUTE FUNCTION protect_derived_asset_request_item_snapshot();

-- +goose StatementBegin
CREATE FUNCTION refresh_derived_asset_batch_counts(target_batch_id UUID)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    aggregate RECORD;
BEGIN
    SELECT
        count(*)::INTEGER AS total_items,
        count(*) FILTER (WHERE disposition = 'executable')::INTEGER AS executable_items,
        count(*) FILTER (WHERE disposition = 'review_required')::INTEGER AS review_required_items,
        count(*) FILTER (WHERE disposition = 'not_found')::INTEGER AS not_found_items,
        count(*) FILTER (WHERE disposition = 'generation_mismatch')::INTEGER AS generation_mismatch_items,
        count(*) FILTER (WHERE disposition = 'already_running')::INTEGER AS already_running_items,
        count(*) FILTER (WHERE disposition = 'duplicate')::INTEGER AS duplicate_items,
        count(*) FILTER (WHERE disposition = 'skipped')::INTEGER AS skipped_items,
        count(*) FILTER (WHERE disposition = 'executable' AND status = 'pending')::INTEGER AS pending_items,
        count(*) FILTER (WHERE disposition = 'executable' AND status = 'queued')::INTEGER AS queued_items,
        count(*) FILTER (WHERE disposition = 'executable' AND status = 'running')::INTEGER AS running_items,
        count(*) FILTER (WHERE disposition = 'executable' AND status = 'succeeded')::INTEGER AS succeeded_items,
        count(*) FILTER (WHERE disposition = 'executable' AND status = 'failed_retryable')::INTEGER AS failed_retryable_items,
        count(*) FILTER (WHERE disposition = 'executable' AND status = 'failed_terminal')::INTEGER AS failed_terminal_items,
        count(*) FILTER (WHERE disposition = 'executable' AND status = 'cancelled')::INTEGER AS cancelled_items,
        count(*) FILTER (WHERE disposition = 'executable' AND status = 'discarded')::INTEGER AS discarded_items
    INTO aggregate
    FROM derived_asset_request_items
    WHERE batch_id = target_batch_id;

    UPDATE derived_asset_batches
    SET total_items = aggregate.total_items,
        executable_items = aggregate.executable_items,
        review_required_items = aggregate.review_required_items,
        not_found_items = aggregate.not_found_items,
        generation_mismatch_items = aggregate.generation_mismatch_items,
        already_running_items = aggregate.already_running_items,
        duplicate_items = aggregate.duplicate_items,
        skipped_items = aggregate.skipped_items,
        pending_items = aggregate.pending_items,
        queued_items = aggregate.queued_items,
        running_items = aggregate.running_items,
        succeeded_items = aggregate.succeeded_items,
        failed_retryable_items = aggregate.failed_retryable_items,
        failed_terminal_items = aggregate.failed_terminal_items,
        cancelled_items = aggregate.cancelled_items,
        discarded_items = aggregate.discarded_items,
        revision = revision + 1
    WHERE id = target_batch_id
      AND ROW(
          total_items,
          executable_items,
          review_required_items,
          not_found_items,
          generation_mismatch_items,
          already_running_items,
          duplicate_items,
          skipped_items,
          pending_items,
          queued_items,
          running_items,
          succeeded_items,
          failed_retryable_items,
          failed_terminal_items,
          cancelled_items,
          discarded_items
      ) IS DISTINCT FROM ROW(
          aggregate.total_items,
          aggregate.executable_items,
          aggregate.review_required_items,
          aggregate.not_found_items,
          aggregate.generation_mismatch_items,
          aggregate.already_running_items,
          aggregate.duplicate_items,
          aggregate.skipped_items,
          aggregate.pending_items,
          aggregate.queued_items,
          aggregate.running_items,
          aggregate.succeeded_items,
          aggregate.failed_retryable_items,
          aggregate.failed_terminal_items,
          aggregate.cancelled_items,
          aggregate.discarded_items
      );
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION refresh_derived_asset_batch_counts_from_item()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM refresh_derived_asset_batch_counts(COALESCE(NEW.batch_id, OLD.batch_id));
    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER derived_asset_request_items_refresh_batch_counts
AFTER INSERT OR DELETE OR UPDATE OF status ON derived_asset_request_items
FOR EACH ROW EXECUTE FUNCTION refresh_derived_asset_batch_counts_from_item();

CREATE TABLE derived_asset_execution_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id UUID NOT NULL,
    request_item_id UUID NOT NULL,
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    workflow_run_id UUID NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    node_run_id UUID,
    node_key TEXT NOT NULL,
    production_generation_id UUID NOT NULL,
    video_production_binding_id UUID NOT NULL,
    video_production_binding_revision BIGINT NOT NULL,
    identity_version SMALLINT NOT NULL DEFAULT 2,
    root_attempt_id UUID REFERENCES derived_asset_execution_items(id) ON DELETE RESTRICT,
    retry_of_attempt_id UUID REFERENCES derived_asset_execution_items(id) ON DELETE RESTRICT,
    attempt_no INTEGER NOT NULL,
    requirement_id UUID NOT NULL,
    storyboard_shot_id UUID NOT NULL,
    canonical_asset_id UUID NOT NULL,
    requirement_snapshot JSONB NOT NULL,
    requirement_snapshot_hash TEXT NOT NULL,
    storyboard_shot_snapshot JSONB NOT NULL,
    storyboard_shot_snapshot_hash TEXT NOT NULL,
    canonical_asset_snapshot JSONB NOT NULL,
    canonical_asset_snapshot_hash TEXT NOT NULL,
    prompt_text TEXT NOT NULL,
    prompt_snapshot JSONB NOT NULL,
    prompt_hash TEXT NOT NULL,
    reference_snapshot JSONB NOT NULL,
    reference_snapshot_hash TEXT NOT NULL,
    model_profile_key TEXT NOT NULL,
    provider_account_id UUID NOT NULL,
    provider_model_id UUID NOT NULL,
    model_snapshot JSONB NOT NULL,
    model_snapshot_hash TEXT NOT NULL,
    capability_snapshot JSONB NOT NULL,
    capability_snapshot_hash TEXT NOT NULL,
    request_snapshot JSONB NOT NULL,
    request_hash TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'prepared',
    revision BIGINT NOT NULL DEFAULT 1,
    execution_token UUID NOT NULL DEFAULT gen_random_uuid(),
    lease_owner TEXT,
    lease_token UUID,
    lease_expires_at TIMESTAMPTZ,
    heartbeat_at TIMESTAMPTZ,
    provider_request_id UUID,
    provider_call_id UUID,
    selected_credential_id UUID,
    provider_result_snapshot JSONB,
    provider_result_hash TEXT,
    artifact_id UUID,
    media_file_id UUID,
    storage_key TEXT,
    output_snapshot JSONB,
    output_hash TEXT,
    error_code TEXT,
    error_message TEXT,
    diagnostic JSONB NOT NULL DEFAULT '{}'::jsonb,
    late_result_count INTEGER NOT NULL DEFAULT 0,
    late_result_diagnostics JSONB NOT NULL DEFAULT '[]'::jsonb,
    last_late_result_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    queued_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT derived_asset_execution_items_batch_fk
        FOREIGN KEY (batch_id, organization_id, project_id)
        REFERENCES derived_asset_batches(id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT derived_asset_execution_items_request_item_fk
        FOREIGN KEY (request_item_id, batch_id, organization_id, project_id)
        REFERENCES derived_asset_request_items(id, batch_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT derived_asset_execution_items_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT derived_asset_execution_items_binding_fk
        FOREIGN KEY (video_production_binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT derived_asset_execution_items_binding_revision_check CHECK (
        video_production_binding_revision > 0
    ),
    CONSTRAINT derived_asset_execution_items_node_key_check CHECK (btrim(node_key) <> ''),
    CONSTRAINT derived_asset_execution_items_identity_version_check CHECK (identity_version = 2),
    CONSTRAINT derived_asset_execution_items_retry_shape_check CHECK (
        (
            attempt_no = 1
            AND root_attempt_id IS NULL
            AND retry_of_attempt_id IS NULL
        ) OR (
            attempt_no > 1
            AND root_attempt_id IS NOT NULL
            AND retry_of_attempt_id IS NOT NULL
            AND root_attempt_id <> id
            AND retry_of_attempt_id <> id
        )
    ),
    CONSTRAINT derived_asset_execution_items_snapshot_types_check CHECK (
        jsonb_typeof(requirement_snapshot) = 'object'
        AND jsonb_typeof(storyboard_shot_snapshot) = 'object'
        AND jsonb_typeof(canonical_asset_snapshot) = 'object'
        AND jsonb_typeof(prompt_snapshot) = 'object'
        AND jsonb_typeof(reference_snapshot) = 'object'
        AND jsonb_typeof(model_snapshot) = 'object'
        AND jsonb_typeof(capability_snapshot) = 'object'
        AND jsonb_typeof(request_snapshot) = 'object'
    ),
    CONSTRAINT derived_asset_execution_items_snapshot_hashes_check CHECK (
        requirement_snapshot_hash ~ '^(sha256:)?[0-9a-f]{64}$'
        AND storyboard_shot_snapshot_hash ~ '^(sha256:)?[0-9a-f]{64}$'
        AND canonical_asset_snapshot_hash ~ '^(sha256:)?[0-9a-f]{64}$'
        AND prompt_hash ~ '^(sha256:)?[0-9a-f]{64}$'
        AND reference_snapshot_hash ~ '^(sha256:)?[0-9a-f]{64}$'
        AND model_snapshot_hash ~ '^(sha256:)?[0-9a-f]{64}$'
        AND capability_snapshot_hash ~ '^(sha256:)?[0-9a-f]{64}$'
        AND request_hash ~ '^(sha256:)?[0-9a-f]{64}$'
    ),
    CONSTRAINT derived_asset_execution_items_prompt_check CHECK (btrim(prompt_text) <> ''),
    CONSTRAINT derived_asset_execution_items_model_profile_check CHECK (btrim(model_profile_key) <> ''),
    CONSTRAINT derived_asset_execution_items_idempotency_key_check CHECK (btrim(idempotency_key) <> ''),
    CONSTRAINT derived_asset_execution_items_status_check CHECK (
        status IN (
            'prepared', 'queued', 'leased', 'provider_running', 'transferring',
            'committing', 'unknown_outcome', 'succeeded', 'failed_retryable',
            'failed_terminal', 'cancelled', 'discarded'
        )
    ),
    CONSTRAINT derived_asset_execution_items_revision_check CHECK (revision > 0),
    CONSTRAINT derived_asset_execution_items_lease_shape_check CHECK (
        (
            status IN ('leased', 'provider_running', 'transferring', 'committing', 'unknown_outcome')
            AND lease_owner IS NOT NULL
            AND btrim(lease_owner) <> ''
            AND lease_token IS NOT NULL
            AND lease_expires_at IS NOT NULL
        ) OR status NOT IN ('leased', 'provider_running', 'transferring', 'committing', 'unknown_outcome')
    ),
    CONSTRAINT derived_asset_execution_items_heartbeat_check CHECK (
        heartbeat_at IS NULL OR lease_expires_at IS NULL OR heartbeat_at <= lease_expires_at
    ),
    CONSTRAINT derived_asset_execution_items_terminal_time_check CHECK (
        (
            status IN ('succeeded', 'failed_retryable', 'failed_terminal', 'cancelled', 'discarded')
            AND completed_at IS NOT NULL
        ) OR (
            status NOT IN ('succeeded', 'failed_retryable', 'failed_terminal', 'cancelled', 'discarded')
            AND completed_at IS NULL
        )
    ),
    CONSTRAINT derived_asset_execution_items_success_shape_check CHECK (
        status <> 'succeeded'
        OR (
            provider_call_id IS NOT NULL
            AND artifact_id IS NOT NULL
            AND media_file_id IS NOT NULL
            AND storage_key IS NOT NULL
            AND btrim(storage_key) <> ''
            AND output_snapshot IS NOT NULL
            AND output_hash IS NOT NULL
            AND error_code IS NULL
            AND error_message IS NULL
        )
    ),
    CONSTRAINT derived_asset_execution_items_failure_shape_check CHECK (
        status NOT IN ('failed_retryable', 'failed_terminal')
        OR (error_code IS NOT NULL AND btrim(error_code) <> '')
    ),
    CONSTRAINT derived_asset_execution_items_provider_result_check CHECK (
        (provider_result_snapshot IS NULL OR jsonb_typeof(provider_result_snapshot) = 'object')
        AND (provider_result_hash IS NULL OR provider_result_hash ~ '^(sha256:)?[0-9a-f]{64}$')
    ),
    CONSTRAINT derived_asset_execution_items_output_check CHECK (
        (output_snapshot IS NULL OR jsonb_typeof(output_snapshot) = 'object')
        AND (output_hash IS NULL OR output_hash ~ '^(sha256:)?[0-9a-f]{64}$')
    ),
    CONSTRAINT derived_asset_execution_items_diagnostic_check CHECK (
        jsonb_typeof(diagnostic) = 'object'
    ),
    CONSTRAINT derived_asset_execution_items_late_result_check CHECK (
        jsonb_typeof(late_result_diagnostics) = 'array'
        AND late_result_count >= 0
        AND jsonb_array_length(late_result_diagnostics) = late_result_count
        AND (
            (late_result_count = 0 AND last_late_result_at IS NULL)
            OR (late_result_count > 0 AND last_late_result_at IS NOT NULL)
        )
    ),
    CONSTRAINT derived_asset_execution_items_item_attempt_unique UNIQUE (request_item_id, attempt_no),
    CONSTRAINT derived_asset_execution_items_identity_unique UNIQUE (
        id, request_item_id, batch_id, organization_id, project_id
    )
);

CREATE UNIQUE INDEX derived_asset_execution_items_idempotency_uidx
    ON derived_asset_execution_items(organization_id, idempotency_key);

CREATE UNIQUE INDEX derived_asset_execution_items_batch_request_hash_uidx
    ON derived_asset_execution_items(batch_id, request_hash, attempt_no);

CREATE UNIQUE INDEX derived_asset_execution_items_one_active_per_item
    ON derived_asset_execution_items(request_item_id)
    WHERE status IN (
        'prepared', 'queued', 'leased', 'provider_running', 'transferring',
        'committing', 'unknown_outcome'
    );

CREATE UNIQUE INDEX derived_asset_execution_items_provider_request_uidx
    ON derived_asset_execution_items(provider_request_id)
    WHERE provider_request_id IS NOT NULL;

CREATE UNIQUE INDEX derived_asset_execution_items_provider_call_uidx
    ON derived_asset_execution_items(provider_call_id)
    WHERE provider_call_id IS NOT NULL;

CREATE UNIQUE INDEX derived_asset_execution_items_artifact_uidx
    ON derived_asset_execution_items(artifact_id)
    WHERE artifact_id IS NOT NULL;

CREATE UNIQUE INDEX derived_asset_execution_items_media_file_uidx
    ON derived_asset_execution_items(media_file_id)
    WHERE media_file_id IS NOT NULL;

CREATE INDEX derived_asset_execution_items_retry_idx
    ON derived_asset_execution_items(root_attempt_id, attempt_no, created_at)
    WHERE root_attempt_id IS NOT NULL;

CREATE INDEX derived_asset_execution_items_stuck_lease_idx
    ON derived_asset_execution_items(lease_expires_at, updated_at, id)
    WHERE status IN ('leased', 'provider_running', 'transferring', 'committing', 'unknown_outcome');

CREATE INDEX derived_asset_execution_items_stuck_queue_idx
    ON derived_asset_execution_items(updated_at, id)
    WHERE status IN ('prepared', 'queued');

CREATE INDEX derived_asset_execution_items_generation_idx
    ON derived_asset_execution_items(
        project_id,
        production_generation_id,
        video_production_binding_id,
        video_production_binding_revision,
        status,
        created_at DESC
    );

-- +goose StatementBegin
CREATE FUNCTION validate_derived_asset_execution_item_lineage()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    parent derived_asset_execution_items%ROWTYPE;
    expected_root UUID;
BEGIN
    IF NEW.retry_of_attempt_id IS NULL THEN
        IF NEW.attempt_no <> 1 OR NEW.root_attempt_id IS NOT NULL THEN
            RAISE EXCEPTION 'invalid root derived asset execution attempt lineage' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    SELECT * INTO parent
    FROM derived_asset_execution_items
    WHERE id = NEW.retry_of_attempt_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'retry source derived asset execution attempt does not exist' USING ERRCODE = '23503';
    END IF;

    expected_root := COALESCE(parent.root_attempt_id, parent.id);
    IF NEW.root_attempt_id IS DISTINCT FROM expected_root
       OR NEW.attempt_no <> parent.attempt_no + 1
       OR NEW.organization_id <> parent.organization_id
       OR NEW.project_id <> parent.project_id
       OR NEW.production_generation_id <> parent.production_generation_id
       OR NEW.video_production_binding_id <> parent.video_production_binding_id
       OR NEW.video_production_binding_revision <> parent.video_production_binding_revision
       OR NEW.requirement_id <> parent.requirement_id
       OR NEW.storyboard_shot_id <> parent.storyboard_shot_id
       OR NEW.canonical_asset_id <> parent.canonical_asset_id
       OR NEW.requirement_snapshot_hash <> parent.requirement_snapshot_hash
       OR NEW.storyboard_shot_snapshot_hash <> parent.storyboard_shot_snapshot_hash
       OR NEW.canonical_asset_snapshot_hash <> parent.canonical_asset_snapshot_hash
       OR NEW.prompt_hash <> parent.prompt_hash
       OR NEW.reference_snapshot_hash <> parent.reference_snapshot_hash
       OR NEW.provider_account_id <> parent.provider_account_id
       OR NEW.provider_model_id <> parent.provider_model_id
       OR NEW.model_snapshot_hash <> parent.model_snapshot_hash
       OR NEW.capability_snapshot_hash <> parent.capability_snapshot_hash
       OR NEW.request_hash <> parent.request_hash THEN
        RAISE EXCEPTION 'derived asset retry attempt identity does not match its parent' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER derived_asset_execution_items_validate_lineage
BEFORE INSERT ON derived_asset_execution_items
FOR EACH ROW EXECUTE FUNCTION validate_derived_asset_execution_item_lineage();

-- +goose StatementBegin
CREATE FUNCTION protect_derived_asset_execution_item_snapshot()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF ROW(
        NEW.batch_id,
        NEW.request_item_id,
        NEW.organization_id,
        NEW.project_id,
        NEW.workflow_run_id,
        NEW.node_run_id,
        NEW.node_key,
        NEW.production_generation_id,
        NEW.video_production_binding_id,
        NEW.video_production_binding_revision,
        NEW.identity_version,
        NEW.root_attempt_id,
        NEW.retry_of_attempt_id,
        NEW.attempt_no,
        NEW.requirement_id,
        NEW.storyboard_shot_id,
        NEW.canonical_asset_id,
        NEW.requirement_snapshot,
        NEW.requirement_snapshot_hash,
        NEW.storyboard_shot_snapshot,
        NEW.storyboard_shot_snapshot_hash,
        NEW.canonical_asset_snapshot,
        NEW.canonical_asset_snapshot_hash,
        NEW.prompt_text,
        NEW.prompt_snapshot,
        NEW.prompt_hash,
        NEW.reference_snapshot,
        NEW.reference_snapshot_hash,
        NEW.model_profile_key,
        NEW.provider_account_id,
        NEW.provider_model_id,
        NEW.model_snapshot,
        NEW.model_snapshot_hash,
        NEW.capability_snapshot,
        NEW.capability_snapshot_hash,
        NEW.request_snapshot,
        NEW.request_hash,
        NEW.idempotency_key,
        NEW.execution_token,
        NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.batch_id,
        OLD.request_item_id,
        OLD.organization_id,
        OLD.project_id,
        OLD.workflow_run_id,
        OLD.node_run_id,
        OLD.node_key,
        OLD.production_generation_id,
        OLD.video_production_binding_id,
        OLD.video_production_binding_revision,
        OLD.identity_version,
        OLD.root_attempt_id,
        OLD.retry_of_attempt_id,
        OLD.attempt_no,
        OLD.requirement_id,
        OLD.storyboard_shot_id,
        OLD.canonical_asset_id,
        OLD.requirement_snapshot,
        OLD.requirement_snapshot_hash,
        OLD.storyboard_shot_snapshot,
        OLD.storyboard_shot_snapshot_hash,
        OLD.canonical_asset_snapshot,
        OLD.canonical_asset_snapshot_hash,
        OLD.prompt_text,
        OLD.prompt_snapshot,
        OLD.prompt_hash,
        OLD.reference_snapshot,
        OLD.reference_snapshot_hash,
        OLD.model_profile_key,
        OLD.provider_account_id,
        OLD.provider_model_id,
        OLD.model_snapshot,
        OLD.model_snapshot_hash,
        OLD.capability_snapshot,
        OLD.capability_snapshot_hash,
        OLD.request_snapshot,
        OLD.request_hash,
        OLD.idempotency_key,
        OLD.execution_token,
        OLD.created_at
    ) THEN
        RAISE EXCEPTION 'derived asset execution attempt snapshot is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status IN ('succeeded', 'failed_retryable', 'failed_terminal', 'cancelled', 'discarded')
       AND NEW.status IS DISTINCT FROM OLD.status THEN
        RAISE EXCEPTION 'terminal derived asset execution attempt status is immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW IS DISTINCT FROM OLD AND NEW.revision <> OLD.revision + 1 THEN
        RAISE EXCEPTION 'derived asset execution attempt update requires the next CAS revision' USING ERRCODE = '40001';
    END IF;
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER derived_asset_execution_items_protect_snapshot
BEFORE UPDATE ON derived_asset_execution_items
FOR EACH ROW EXECUTE FUNCTION protect_derived_asset_execution_item_snapshot();

-- +goose StatementBegin
CREATE FUNCTION sync_derived_asset_request_item_from_attempt()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    mapped_outcome TEXT;
BEGIN
    mapped_outcome := CASE
        WHEN NEW.status = 'prepared' THEN 'pending'
        WHEN NEW.status = 'queued' THEN 'queued'
        WHEN NEW.status IN ('leased', 'provider_running', 'transferring', 'committing', 'unknown_outcome') THEN 'running'
        ELSE NEW.status
    END;

    UPDATE derived_asset_request_items
    SET current_attempt_id = NEW.id,
        current_attempt_no = NEW.attempt_no,
        status = mapped_outcome,
        error_code = CASE
            WHEN mapped_outcome IN ('failed_retryable', 'failed_terminal', 'cancelled', 'discarded')
            THEN NEW.error_code
            ELSE NULL
        END,
        error_message = CASE
            WHEN mapped_outcome IN ('failed_retryable', 'failed_terminal', 'cancelled', 'discarded')
            THEN NEW.error_message
            ELSE NULL
        END,
        revision = revision + 1
    WHERE id = NEW.request_item_id
      AND disposition = 'executable'
      AND (current_attempt_no IS NULL OR current_attempt_no <= NEW.attempt_no)
      AND ROW(current_attempt_id, current_attempt_no, status, error_code, error_message)
          IS DISTINCT FROM ROW(
            NEW.id,
            NEW.attempt_no,
            mapped_outcome,
            CASE WHEN mapped_outcome IN ('failed_retryable', 'failed_terminal', 'cancelled', 'discarded') THEN NEW.error_code ELSE NULL END,
            CASE WHEN mapped_outcome IN ('failed_retryable', 'failed_terminal', 'cancelled', 'discarded') THEN NEW.error_message ELSE NULL END
          );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER derived_asset_execution_items_sync_request_item
AFTER INSERT OR UPDATE OF status ON derived_asset_execution_items
FOR EACH ROW EXECUTE FUNCTION sync_derived_asset_request_item_from_attempt();

-- +goose Down

SET search_path TO public;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM derived_asset_batches LIMIT 1) THEN
        RAISE EXCEPTION 'cannot roll back derived asset execution v2 while execution history exists';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS derived_asset_execution_items_sync_request_item ON derived_asset_execution_items;
DROP FUNCTION IF EXISTS sync_derived_asset_request_item_from_attempt();
DROP TRIGGER IF EXISTS derived_asset_execution_items_protect_snapshot ON derived_asset_execution_items;
DROP FUNCTION IF EXISTS protect_derived_asset_execution_item_snapshot();
DROP TRIGGER IF EXISTS derived_asset_execution_items_validate_lineage ON derived_asset_execution_items;
DROP FUNCTION IF EXISTS validate_derived_asset_execution_item_lineage();
DROP TABLE IF EXISTS derived_asset_execution_items;

DROP TRIGGER IF EXISTS derived_asset_request_items_refresh_batch_counts ON derived_asset_request_items;
DROP FUNCTION IF EXISTS refresh_derived_asset_batch_counts_from_item();
DROP FUNCTION IF EXISTS refresh_derived_asset_batch_counts(UUID);
DROP TRIGGER IF EXISTS derived_asset_request_items_protect_snapshot ON derived_asset_request_items;
DROP FUNCTION IF EXISTS protect_derived_asset_request_item_snapshot();
DROP TRIGGER IF EXISTS derived_asset_request_items_validate_identity ON derived_asset_request_items;
DROP FUNCTION IF EXISTS validate_derived_asset_request_item_identity();
DROP TABLE IF EXISTS derived_asset_request_items;

DROP TRIGGER IF EXISTS derived_asset_batches_protect_identity ON derived_asset_batches;
DROP FUNCTION IF EXISTS protect_derived_asset_batch_identity();
DROP TRIGGER IF EXISTS derived_asset_batches_validate_lineage ON derived_asset_batches;
DROP FUNCTION IF EXISTS validate_derived_asset_batch_lineage();
DROP TABLE IF EXISTS derived_asset_batches;
