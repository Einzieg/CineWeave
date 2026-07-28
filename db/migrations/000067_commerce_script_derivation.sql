-- +goose Up

SET search_path TO public;

ALTER TABLE commerce_products
    ADD COLUMN next_script_sort_order BIGINT NOT NULL DEFAULT 10;

UPDATE commerce_products product
SET next_script_sort_order = greatest(
    product.next_script_sort_order,
    COALESCE((
        SELECT max(unit.sort_order) + 10
        FROM commerce_script_units unit
        WHERE unit.product_id = product.id
    ), 10)
);

ALTER TABLE commerce_products
    ADD CONSTRAINT commerce_products_next_sort_order_check
    CHECK (next_script_sort_order > 0);

ALTER TABLE commerce_script_units
    DROP CONSTRAINT commerce_script_units_derivation_check,
    ADD CONSTRAINT commerce_script_units_derivation_check CHECK (
        (derived_from_script_unit_id IS NULL AND derivation_kind IS NULL)
        OR (
            derived_from_script_unit_id IS NOT NULL
            AND derivation_kind IN (
                'copy',
                'language_variant',
                'agent_idea',
                'scene_variant',
                'hook_variant',
                'audience_variant',
                'tone_variant',
                'cta_variant',
                'custom_variant'
            )
        )
    );

CREATE TABLE commerce_script_derivation_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    source_script_unit_id UUID NOT NULL,
    source_content_snapshot TEXT NOT NULL,
    source_content_hash TEXT NOT NULL,
    product_version_id UUID NOT NULL,
    product_snapshot_hash TEXT NOT NULL,
    production_generation_id UUID NOT NULL,
    video_production_binding_id UUID NOT NULL,
    video_production_binding_revision BIGINT NOT NULL,
    production_configuration_hash TEXT NOT NULL,
    script_model_profile_key TEXT NOT NULL,
    model_profile_binding_id UUID NOT NULL,
    model_profile_binding_revision BIGINT NOT NULL,
    provider_model_id UUID NOT NULL,
    routing_snapshot_hash TEXT NOT NULL,
    prompt_contract_snapshot JSONB NOT NULL,
    dimension TEXT NOT NULL,
    instruction TEXT NOT NULL,
    preserve_contract JSONB NOT NULL,
    variation_plan JSONB NOT NULL,
    requested_count INTEGER NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    root_batch_id UUID,
    retry_of_batch_id UUID,
    retry_depth INTEGER NOT NULL DEFAULT 0,
    workflow_run_id UUID,
    status TEXT NOT NULL DEFAULT 'queued',
    queued_count INTEGER NOT NULL DEFAULT 0,
    running_count INTEGER NOT NULL DEFAULT 0,
    succeeded_count INTEGER NOT NULL DEFAULT 0,
    failed_retryable_count INTEGER NOT NULL DEFAULT 0,
    failed_terminal_count INTEGER NOT NULL DEFAULT 0,
    cancelled_count INTEGER NOT NULL DEFAULT 0,
    revision BIGINT NOT NULL DEFAULT 1,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_script_derivation_batches_product_fk
        FOREIGN KEY (product_id, organization_id, project_id)
        REFERENCES commerce_products(id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_derivation_batches_source_unit_fk
        FOREIGN KEY (source_script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_units(id, product_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_derivation_batches_product_version_fk
        FOREIGN KEY (product_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_versions(id, product_id, organization_id, project_id)
        ON DELETE NO ACTION DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT commerce_script_derivation_batches_generation_fk
        FOREIGN KEY (production_generation_id, project_id, organization_id)
        REFERENCES project_video_production_generations(id, project_id, organization_id)
        ON DELETE NO ACTION DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT commerce_script_derivation_batches_video_binding_fk
        FOREIGN KEY (video_production_binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id)
        ON DELETE NO ACTION DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT commerce_script_derivation_batches_model_binding_fk
        FOREIGN KEY (model_profile_binding_id)
        REFERENCES model_profile_bindings(id) ON DELETE SET NULL,
    CONSTRAINT commerce_script_derivation_batches_provider_model_fk
        FOREIGN KEY (provider_model_id)
        REFERENCES provider_models(id) ON DELETE SET NULL,
    CONSTRAINT commerce_script_derivation_batches_workflow_fk
        FOREIGN KEY (workflow_run_id)
        REFERENCES workflow_runs(id) ON DELETE SET NULL
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT commerce_script_derivation_batches_root_fk
        FOREIGN KEY (root_batch_id)
        REFERENCES commerce_script_derivation_batches(id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_derivation_batches_retry_fk
        FOREIGN KEY (retry_of_batch_id)
        REFERENCES commerce_script_derivation_batches(id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_derivation_batches_text_check CHECK (
        btrim(source_content_snapshot) <> ''
        AND btrim(script_model_profile_key) <> ''
        AND btrim(dimension) <> ''
        AND btrim(instruction) <> ''
        AND btrim(idempotency_key) <> ''
    ),
    CONSTRAINT commerce_script_derivation_batches_hash_check CHECK (
        source_content_hash ~ '^[0-9a-f]{64}$'
        AND product_snapshot_hash ~ '^[0-9a-f]{64}$'
        AND production_configuration_hash ~ '^[0-9a-f]{64}$'
        AND routing_snapshot_hash ~ '^[0-9a-f]{64}$'
        AND request_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT commerce_script_derivation_batches_json_check CHECK (
        jsonb_typeof(prompt_contract_snapshot) = 'object'
        AND jsonb_typeof(preserve_contract) = 'array'
        AND jsonb_typeof(variation_plan) = 'array'
    ),
    CONSTRAINT commerce_script_derivation_batches_dimension_check CHECK (
        dimension IN ('scene', 'hook', 'audience', 'tone', 'language', 'cta', 'custom')
    ),
    CONSTRAINT commerce_script_derivation_batches_status_check CHECK (
        status IN ('queued', 'running', 'partial_succeeded', 'succeeded', 'failed', 'cancelling', 'cancelled')
    ),
    CONSTRAINT commerce_script_derivation_batches_count_check CHECK (
        requested_count BETWEEN 1 AND 20
        AND queued_count >= 0 AND running_count >= 0
        AND succeeded_count >= 0 AND failed_retryable_count >= 0
        AND failed_terminal_count >= 0 AND cancelled_count >= 0
    ),
    CONSTRAINT commerce_script_derivation_batches_identity_check CHECK (
        video_production_binding_revision > 0
        AND model_profile_binding_revision > 0
        AND retry_depth >= 0
        AND revision > 0
    ),
    CONSTRAINT commerce_script_derivation_batches_lineage_check CHECK (
        (retry_depth = 0 AND retry_of_batch_id IS NULL)
        OR (retry_depth > 0 AND retry_of_batch_id IS NOT NULL)
    ),
    CONSTRAINT commerce_script_derivation_batches_terminal_check CHECK (
        (status IN ('partial_succeeded', 'succeeded', 'failed', 'cancelled') AND completed_at IS NOT NULL)
        OR (status NOT IN ('partial_succeeded', 'succeeded', 'failed', 'cancelled') AND completed_at IS NULL)
    ),
    UNIQUE(organization_id, idempotency_key),
    UNIQUE(id, organization_id, project_id),
    UNIQUE(id, product_id, organization_id, project_id)
);

ALTER TABLE commerce_script_derivation_batches
    ALTER COLUMN model_profile_binding_id DROP NOT NULL,
    ALTER COLUMN provider_model_id DROP NOT NULL;

CREATE INDEX commerce_script_derivation_batches_project_idx
    ON commerce_script_derivation_batches(project_id, created_at DESC, id DESC);

CREATE INDEX commerce_script_derivation_batches_source_idx
    ON commerce_script_derivation_batches(source_script_unit_id, created_at DESC, id DESC);

CREATE INDEX commerce_script_derivation_batches_active_idx
    ON commerce_script_derivation_batches(status, updated_at)
    WHERE status IN ('queued', 'running', 'cancelling');

CREATE TABLE commerce_script_derivation_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id UUID NOT NULL,
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    input_ordinal INTEGER NOT NULL,
    root_item_id UUID,
    retry_of_item_id UUID,
    variation_key TEXT NOT NULL,
    variation_label TEXT NOT NULL,
    variation_brief TEXT NOT NULL,
    input_snapshot JSONB NOT NULL,
    input_hash TEXT NOT NULL,
    reserved_unit_no BIGINT NOT NULL,
    reserved_sort_order BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    current_attempt_id UUID,
    output_script_unit_id UUID,
    output_script_version_id UUID,
    error_code TEXT,
    error_message TEXT,
    revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_script_derivation_items_batch_fk
        FOREIGN KEY (batch_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_derivation_batches(id, product_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_derivation_items_root_fk
        FOREIGN KEY (root_item_id)
        REFERENCES commerce_script_derivation_items(id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_derivation_items_retry_fk
        FOREIGN KEY (retry_of_item_id)
        REFERENCES commerce_script_derivation_items(id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_derivation_items_output_unit_fk
        FOREIGN KEY (output_script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_units(id, product_id, organization_id, project_id)
        ON DELETE SET NULL (output_script_unit_id),
    CONSTRAINT commerce_script_derivation_items_output_version_fk
        FOREIGN KEY (output_script_version_id)
        REFERENCES commerce_ad_script_versions(id) ON DELETE SET NULL,
    CONSTRAINT commerce_script_derivation_items_text_check CHECK (
        btrim(variation_key) <> ''
        AND btrim(variation_label) <> ''
        AND btrim(variation_brief) <> ''
    ),
    CONSTRAINT commerce_script_derivation_items_hash_check CHECK (
        input_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT commerce_script_derivation_items_json_check CHECK (
        jsonb_typeof(input_snapshot) = 'object'
    ),
    CONSTRAINT commerce_script_derivation_items_status_check CHECK (
        status IN ('queued', 'running', 'reviewing', 'succeeded', 'failed_retryable', 'failed_terminal', 'cancelled')
    ),
    CONSTRAINT commerce_script_derivation_items_identity_check CHECK (
        input_ordinal > 0 AND reserved_unit_no > 0 AND reserved_sort_order > 0 AND revision > 0
    ),
    CONSTRAINT commerce_script_derivation_items_output_check CHECK (
        (status = 'succeeded' AND output_script_unit_id IS NOT NULL AND output_script_version_id IS NOT NULL)
        OR status <> 'succeeded'
    ),
    CONSTRAINT commerce_script_derivation_items_failure_check CHECK (
        (status IN ('failed_retryable', 'failed_terminal') AND error_code IS NOT NULL AND error_message IS NOT NULL)
        OR status NOT IN ('failed_retryable', 'failed_terminal')
    ),
    CONSTRAINT commerce_script_derivation_items_terminal_check CHECK (
        (status IN ('succeeded', 'failed_retryable', 'failed_terminal', 'cancelled') AND completed_at IS NOT NULL)
        OR (status NOT IN ('succeeded', 'failed_retryable', 'failed_terminal', 'cancelled') AND completed_at IS NULL)
    ),
    UNIQUE(batch_id, input_ordinal),
    UNIQUE(batch_id, variation_key),
    UNIQUE(id, batch_id),
    UNIQUE(id, product_id, organization_id, project_id)
);

CREATE INDEX commerce_script_derivation_items_batch_idx
    ON commerce_script_derivation_items(batch_id, input_ordinal);

CREATE INDEX commerce_script_derivation_items_active_idx
    ON commerce_script_derivation_items(status, updated_at)
    WHERE status IN ('queued', 'running', 'reviewing');

CREATE TABLE commerce_script_derivation_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id UUID NOT NULL,
    item_id UUID NOT NULL,
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    attempt_no INTEGER NOT NULL,
    root_attempt_id UUID,
    retry_of_attempt_id UUID,
    status TEXT NOT NULL DEFAULT 'queued',
    final_output_content_hash TEXT,
    review_round INTEGER NOT NULL DEFAULT 0,
    review_result JSONB NOT NULL DEFAULT '{}'::jsonb,
    review_feedback TEXT,
    error_code TEXT,
    error_message TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_script_derivation_attempts_item_fk
        FOREIGN KEY (item_id, batch_id)
        REFERENCES commerce_script_derivation_items(id, batch_id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_derivation_attempts_root_fk
        FOREIGN KEY (root_attempt_id)
        REFERENCES commerce_script_derivation_attempts(id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_derivation_attempts_retry_fk
        FOREIGN KEY (retry_of_attempt_id)
        REFERENCES commerce_script_derivation_attempts(id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_derivation_attempts_status_check CHECK (
        status IN ('queued', 'generating', 'reviewing', 'revising', 'succeeded', 'failed', 'cancelled')
    ),
    CONSTRAINT commerce_script_derivation_attempts_identity_check CHECK (
        attempt_no > 0 AND review_round BETWEEN 0 AND 3
    ),
    CONSTRAINT commerce_script_derivation_attempts_hash_check CHECK (
        final_output_content_hash IS NULL OR final_output_content_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT commerce_script_derivation_attempts_json_check CHECK (
        jsonb_typeof(review_result) = 'object'
    ),
    CONSTRAINT commerce_script_derivation_attempts_failure_check CHECK (
        (status = 'failed' AND error_code IS NOT NULL AND error_message IS NOT NULL)
        OR status <> 'failed'
    ),
    CONSTRAINT commerce_script_derivation_attempts_terminal_check CHECK (
        (status IN ('succeeded', 'failed', 'cancelled') AND completed_at IS NOT NULL)
        OR (status NOT IN ('succeeded', 'failed', 'cancelled') AND completed_at IS NULL)
    ),
    UNIQUE(item_id, attempt_no),
    UNIQUE(id, item_id)
);

ALTER TABLE commerce_script_derivation_items
    ADD CONSTRAINT commerce_script_derivation_items_attempt_fk
    FOREIGN KEY (current_attempt_id, id)
    REFERENCES commerce_script_derivation_attempts(id, item_id)
    ON DELETE SET NULL (current_attempt_id);

CREATE TABLE commerce_script_derivation_attempt_calls (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id UUID NOT NULL,
    item_id UUID NOT NULL,
    attempt_id UUID NOT NULL,
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    round_no INTEGER NOT NULL,
    phase TEXT NOT NULL,
    provider_request_id UUID,
    provider_call_id UUID,
    model_profile_key TEXT NOT NULL,
    model_profile_binding_id UUID,
    provider_model_id UUID,
    prompt_template_key TEXT NOT NULL,
    prompt_version_id UUID NOT NULL,
    prompt_hash TEXT NOT NULL,
    output_content_hash TEXT,
    status TEXT NOT NULL,
    error_code TEXT,
    error_message TEXT,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_script_derivation_attempt_calls_attempt_fk
        FOREIGN KEY (attempt_id, item_id)
        REFERENCES commerce_script_derivation_attempts(id, item_id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_derivation_attempt_calls_provider_request_fk
        FOREIGN KEY (provider_request_id)
        REFERENCES provider_requests(id) ON DELETE SET NULL,
    CONSTRAINT commerce_script_derivation_attempt_calls_provider_call_fk
        FOREIGN KEY (provider_call_id)
        REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    CONSTRAINT commerce_script_derivation_attempt_calls_model_binding_fk
        FOREIGN KEY (model_profile_binding_id)
        REFERENCES model_profile_bindings(id) ON DELETE SET NULL,
    CONSTRAINT commerce_script_derivation_attempt_calls_provider_model_fk
        FOREIGN KEY (provider_model_id)
        REFERENCES provider_models(id) ON DELETE SET NULL,
    CONSTRAINT commerce_script_derivation_attempt_calls_prompt_version_fk
        FOREIGN KEY (prompt_version_id)
        REFERENCES prompt_versions(id) ON DELETE NO ACTION,
    CONSTRAINT commerce_script_derivation_attempt_calls_phase_check CHECK (
        phase IN ('generate', 'review', 'revise')
    ),
    CONSTRAINT commerce_script_derivation_attempt_calls_status_check CHECK (
        status IN ('running', 'succeeded', 'failed')
    ),
    CONSTRAINT commerce_script_derivation_attempt_calls_identity_check CHECK (
        round_no BETWEEN 1 AND 3
        AND btrim(model_profile_key) <> ''
        AND btrim(prompt_template_key) <> ''
    ),
    CONSTRAINT commerce_script_derivation_attempt_calls_hash_check CHECK (
        prompt_hash ~ '^[0-9a-f]{64}$'
        AND (output_content_hash IS NULL OR output_content_hash ~ '^[0-9a-f]{64}$')
    ),
    CONSTRAINT commerce_script_derivation_attempt_calls_failure_check CHECK (
        (status = 'failed' AND error_code IS NOT NULL AND error_message IS NOT NULL)
        OR status <> 'failed'
    ),
    CONSTRAINT commerce_script_derivation_attempt_calls_terminal_check CHECK (
        (status IN ('succeeded', 'failed') AND completed_at IS NOT NULL)
        OR (status = 'running' AND completed_at IS NULL)
    ),
    UNIQUE(attempt_id, round_no, phase)
);

CREATE INDEX commerce_script_derivation_attempt_calls_attempt_idx
    ON commerce_script_derivation_attempt_calls(attempt_id, round_no, phase);

-- +goose Down

SET search_path TO public;

DROP TABLE IF EXISTS commerce_script_derivation_attempt_calls;

ALTER TABLE commerce_script_derivation_items
    DROP CONSTRAINT IF EXISTS commerce_script_derivation_items_attempt_fk;

DROP TABLE IF EXISTS commerce_script_derivation_attempts;
DROP TABLE IF EXISTS commerce_script_derivation_items;
DROP TABLE IF EXISTS commerce_script_derivation_batches;

UPDATE commerce_script_units
SET derivation_kind = 'agent_idea'
WHERE derivation_kind IN (
    'scene_variant',
    'hook_variant',
    'audience_variant',
    'tone_variant',
    'cta_variant',
    'custom_variant'
);

ALTER TABLE commerce_script_units
    DROP CONSTRAINT commerce_script_units_derivation_check,
    ADD CONSTRAINT commerce_script_units_derivation_check CHECK (
        (derived_from_script_unit_id IS NULL AND derivation_kind IS NULL)
        OR (
            derived_from_script_unit_id IS NOT NULL
            AND derivation_kind IN ('copy', 'language_variant', 'agent_idea')
        )
    );

ALTER TABLE commerce_products
    DROP CONSTRAINT IF EXISTS commerce_products_next_sort_order_check,
    DROP COLUMN IF EXISTS next_script_sort_order;
