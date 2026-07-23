-- +goose Up

SET search_path TO public;

CREATE TABLE commerce_sales_script_contracts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    script_unit_generation_id UUID NOT NULL,
    project_production_generation_id UUID NOT NULL,
    commerce_workflow_binding_id UUID NOT NULL,
    commerce_workflow_binding_revision BIGINT NOT NULL,
    product_version_id UUID NOT NULL,
    source_script_version_id UUID NOT NULL,
    localization_id UUID NOT NULL,
    reference_pack_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'running',
    attempt_generation INTEGER NOT NULL DEFAULT 1,
    current_workflow_run_id UUID NOT NULL REFERENCES workflow_runs(id) ON DELETE RESTRICT,
    input_hash TEXT NOT NULL,
    contract_version TEXT,
    contract JSONB,
    contract_hash TEXT,
    prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE SET NULL,
    provider_request_id UUID REFERENCES provider_requests(id) ON DELETE SET NULL,
    provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    provider_model_id UUID REFERENCES provider_models(id) ON DELETE SET NULL,
    accepted_round INTEGER,
    error_code TEXT,
    error_message TEXT,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_sales_script_contracts_generation_fk
        FOREIGN KEY (script_unit_generation_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_sales_script_contracts_project_generation_fk
        FOREIGN KEY (project_production_generation_id, project_id, organization_id)
        REFERENCES project_video_production_generations(id, project_id, organization_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_sales_script_contracts_binding_fk
        FOREIGN KEY (commerce_workflow_binding_id, project_id, organization_id, commerce_workflow_binding_revision)
        REFERENCES project_commerce_workflow_bindings(id, project_id, organization_id, binding_revision) ON DELETE RESTRICT,
    CONSTRAINT commerce_sales_script_contracts_product_version_fk
        FOREIGN KEY (product_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_versions(id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_sales_script_contracts_script_version_fk
        FOREIGN KEY (source_script_version_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_versions(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_sales_script_contracts_localization_fk
        FOREIGN KEY (localization_id, script_unit_id, source_script_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_localizations(id, script_unit_id, source_script_version_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_sales_script_contracts_reference_pack_fk
        FOREIGN KEY (reference_pack_id, product_id, product_version_id, organization_id, project_id)
        REFERENCES commerce_product_reference_packs(id, product_id, product_version_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_sales_script_contracts_status_check
        CHECK (status IN ('running', 'ready', 'failed', 'cancelled')),
    CONSTRAINT commerce_sales_script_contracts_attempt_check CHECK (attempt_generation > 0),
    CONSTRAINT commerce_sales_script_contracts_binding_revision_check CHECK (commerce_workflow_binding_revision > 0),
    CONSTRAINT commerce_sales_script_contracts_input_hash_check CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_sales_script_contracts_contract_hash_check CHECK (
        contract_hash IS NULL OR contract_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT commerce_sales_script_contracts_contract_json_check CHECK (
        contract IS NULL OR jsonb_typeof(contract) = 'object'
    ),
    CONSTRAINT commerce_sales_script_contracts_lifecycle_check CHECK (
        (
            status = 'ready'
            AND completed_at IS NOT NULL
            AND contract_version IS NOT NULL
            AND contract IS NOT NULL
            AND contract_hash IS NOT NULL
            AND prompt_version_id IS NOT NULL
            AND provider_call_id IS NOT NULL
            AND accepted_round IS NOT NULL
            AND error_code IS NULL
            AND error_message IS NULL
        )
        OR (
            status IN ('failed', 'cancelled')
            AND completed_at IS NOT NULL
            AND error_code IS NOT NULL
            AND contract IS NULL
            AND contract_hash IS NULL
        )
        OR (
            status = 'running'
            AND completed_at IS NULL
            AND error_code IS NULL
            AND error_message IS NULL
            AND contract IS NULL
            AND contract_hash IS NULL
        )
    ),
    UNIQUE(script_unit_generation_id),
    UNIQUE(id, script_unit_id, script_unit_generation_id, organization_id, project_id, contract_hash)
);

CREATE INDEX commerce_sales_script_contracts_unit_status_idx
    ON commerce_sales_script_contracts(script_unit_id, status, updated_at DESC);

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_sales_script_contract()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.organization_id IS DISTINCT FROM OLD.organization_id
       OR NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.product_id IS DISTINCT FROM OLD.product_id
       OR NEW.script_unit_id IS DISTINCT FROM OLD.script_unit_id
       OR NEW.script_unit_generation_id IS DISTINCT FROM OLD.script_unit_generation_id
       OR NEW.project_production_generation_id IS DISTINCT FROM OLD.project_production_generation_id
       OR NEW.commerce_workflow_binding_id IS DISTINCT FROM OLD.commerce_workflow_binding_id
       OR NEW.commerce_workflow_binding_revision IS DISTINCT FROM OLD.commerce_workflow_binding_revision
       OR NEW.product_version_id IS DISTINCT FROM OLD.product_version_id
       OR NEW.source_script_version_id IS DISTINCT FROM OLD.source_script_version_id
       OR NEW.localization_id IS DISTINCT FROM OLD.localization_id
       OR NEW.reference_pack_id IS DISTINCT FROM OLD.reference_pack_id
       OR NEW.input_hash IS DISTINCT FROM OLD.input_hash
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'commerce sales script contract identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'ready' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'ready commerce sales script contracts are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.contract IS NOT NULL AND (
        NEW.contract IS DISTINCT FROM OLD.contract
        OR NEW.contract_hash IS DISTINCT FROM OLD.contract_hash
        OR NEW.contract_version IS DISTINCT FROM OLD.contract_version
        OR NEW.prompt_version_id IS DISTINCT FROM OLD.prompt_version_id
        OR NEW.provider_request_id IS DISTINCT FROM OLD.provider_request_id
        OR NEW.provider_call_id IS DISTINCT FROM OLD.provider_call_id
        OR NEW.provider_model_id IS DISTINCT FROM OLD.provider_model_id
        OR NEW.accepted_round IS DISTINCT FROM OLD.accepted_round
    ) THEN
        RAISE EXCEPTION 'commerce sales script contract result is immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.attempt_generation < OLD.attempt_generation THEN
        RAISE EXCEPTION 'commerce sales script contract attempts are monotonic' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_sales_script_contracts_immutable
BEFORE UPDATE ON commerce_sales_script_contracts
FOR EACH ROW EXECUTE FUNCTION protect_commerce_sales_script_contract();

ALTER TABLE commerce_storyboard_plans
    ADD COLUMN sales_script_contract_id UUID NOT NULL,
    ADD COLUMN sales_script_contract_hash TEXT NOT NULL,
    ADD CONSTRAINT commerce_storyboard_plans_sales_script_contract_fk
        FOREIGN KEY (sales_script_contract_id, script_unit_id, script_unit_generation_id, organization_id, project_id, sales_script_contract_hash)
        REFERENCES commerce_sales_script_contracts(id, script_unit_id, script_unit_generation_id, organization_id, project_id, contract_hash) ON DELETE RESTRICT,
    ADD CONSTRAINT commerce_storyboard_plans_sales_script_contract_hash_check
        CHECK (sales_script_contract_hash ~ '^[0-9a-f]{64}$');

CREATE TABLE commerce_script_unit_rebuilds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    project_production_generation_id UUID NOT NULL,
    source_unit_generation_id UUID NOT NULL,
    source_unit_configuration_hash TEXT NOT NULL,
    source_script_version_id UUID NOT NULL,
    source_localization_id UUID NOT NULL,
    target_source_script_version_id UUID NOT NULL,
    target_language_mode TEXT NOT NULL,
    target_explicit_language TEXT,
    target_duration_seconds INTEGER NOT NULL,
    target_platform TEXT NOT NULL,
    target_configuration_snapshot JSONB NOT NULL,
    target_configuration_hash TEXT NOT NULL,
    impact_snapshot JSONB NOT NULL,
    impact_token TEXT NOT NULL,
    impact_expires_at TIMESTAMPTZ NOT NULL,
    expected_script_unit_revision BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'planned',
    idempotency_key TEXT NOT NULL,
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    target_localization_id UUID,
    target_unit_generation_id UUID,
    requested_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    error_code TEXT,
    error_message TEXT,
    CONSTRAINT commerce_script_unit_rebuilds_unit_fk
        FOREIGN KEY (script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_units(id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_script_unit_rebuilds_generation_fk
        FOREIGN KEY (project_production_generation_id, project_id, organization_id)
        REFERENCES project_video_production_generations(id, project_id, organization_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_script_unit_rebuilds_source_generation_fk
        FOREIGN KEY (source_unit_generation_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_script_unit_rebuilds_source_version_fk
        FOREIGN KEY (source_script_version_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_versions(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_script_unit_rebuilds_source_localization_fk
        FOREIGN KEY (source_localization_id, script_unit_id, source_script_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_localizations(id, script_unit_id, source_script_version_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_script_unit_rebuilds_target_version_fk
        FOREIGN KEY (target_source_script_version_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_versions(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_script_unit_rebuilds_target_localization_fk
        FOREIGN KEY (target_localization_id, script_unit_id, target_source_script_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_localizations(id, script_unit_id, source_script_version_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_script_unit_rebuilds_target_generation_fk
        FOREIGN KEY (target_unit_generation_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_script_unit_rebuilds_source_hash_check CHECK (source_unit_configuration_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_script_unit_rebuilds_target_hash_check CHECK (target_configuration_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_script_unit_rebuilds_token_check CHECK (impact_token ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_script_unit_rebuilds_snapshot_check CHECK (
        jsonb_typeof(target_configuration_snapshot) = 'object'
        AND jsonb_typeof(impact_snapshot) = 'object'
    ),
    CONSTRAINT commerce_script_unit_rebuilds_language_check CHECK (
        (target_language_mode = 'auto' AND target_explicit_language IS NULL)
        OR (
            target_language_mode = 'explicit'
            AND target_explicit_language ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
        )
    ),
    CONSTRAINT commerce_script_unit_rebuilds_duration_check CHECK (target_duration_seconds IN (15, 30, 60)),
    CONSTRAINT commerce_script_unit_rebuilds_platform_check CHECK (trim(target_platform) <> ''),
    CONSTRAINT commerce_script_unit_rebuilds_revision_check CHECK (expected_script_unit_revision > 0),
    CONSTRAINT commerce_script_unit_rebuilds_status_check
        CHECK (status IN ('planned', 'running', 'waiting_user_confirmation', 'succeeded', 'failed', 'cancelled')),
    CONSTRAINT commerce_script_unit_rebuilds_key_check CHECK (trim(idempotency_key) <> ''),
    CONSTRAINT commerce_script_unit_rebuilds_terminal_check CHECK (
        (
            status = 'succeeded'
            AND completed_at IS NOT NULL
            AND target_localization_id IS NOT NULL
            AND target_unit_generation_id IS NOT NULL
            AND error_code IS NULL
            AND error_message IS NULL
        )
        OR (
            status IN ('failed', 'cancelled')
            AND completed_at IS NOT NULL
            AND error_code IS NOT NULL
        )
        OR (
            status IN ('planned', 'running', 'waiting_user_confirmation')
            AND completed_at IS NULL
            AND target_unit_generation_id IS NULL
        )
    ),
    UNIQUE(organization_id, impact_token),
    UNIQUE(organization_id, idempotency_key),
    UNIQUE(id, script_unit_id, organization_id, project_id)
);

CREATE UNIQUE INDEX commerce_script_unit_rebuilds_one_open
    ON commerce_script_unit_rebuilds(script_unit_id)
    WHERE status IN ('planned', 'running', 'waiting_user_confirmation');

CREATE INDEX commerce_script_unit_rebuilds_unit_status_idx
    ON commerce_script_unit_rebuilds(script_unit_id, status, created_at DESC);

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_script_unit_rebuild_snapshot()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.organization_id IS DISTINCT FROM OLD.organization_id
       OR NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.product_id IS DISTINCT FROM OLD.product_id
       OR NEW.script_unit_id IS DISTINCT FROM OLD.script_unit_id
       OR NEW.project_production_generation_id IS DISTINCT FROM OLD.project_production_generation_id
       OR NEW.source_unit_generation_id IS DISTINCT FROM OLD.source_unit_generation_id
       OR NEW.source_unit_configuration_hash IS DISTINCT FROM OLD.source_unit_configuration_hash
       OR NEW.source_script_version_id IS DISTINCT FROM OLD.source_script_version_id
       OR NEW.source_localization_id IS DISTINCT FROM OLD.source_localization_id
       OR NEW.target_source_script_version_id IS DISTINCT FROM OLD.target_source_script_version_id
       OR NEW.target_language_mode IS DISTINCT FROM OLD.target_language_mode
       OR NEW.target_explicit_language IS DISTINCT FROM OLD.target_explicit_language
       OR NEW.target_duration_seconds IS DISTINCT FROM OLD.target_duration_seconds
       OR NEW.target_platform IS DISTINCT FROM OLD.target_platform
       OR NEW.target_configuration_snapshot IS DISTINCT FROM OLD.target_configuration_snapshot
       OR NEW.target_configuration_hash IS DISTINCT FROM OLD.target_configuration_hash
       OR NEW.impact_snapshot IS DISTINCT FROM OLD.impact_snapshot
       OR NEW.impact_token IS DISTINCT FROM OLD.impact_token
       OR NEW.impact_expires_at IS DISTINCT FROM OLD.impact_expires_at
       OR NEW.expected_script_unit_revision IS DISTINCT FROM OLD.expected_script_unit_revision
       OR NEW.requested_by IS DISTINCT FROM OLD.requested_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'commerce script unit rebuild snapshot is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status IN ('succeeded', 'failed', 'cancelled') AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'terminal commerce script unit rebuilds are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_script_unit_rebuilds_snapshot_immutable
BEFORE UPDATE ON commerce_script_unit_rebuilds
FOR EACH ROW EXECUTE FUNCTION protect_commerce_script_unit_rebuild_snapshot();

-- +goose Down

SET search_path TO public;

DROP TRIGGER IF EXISTS commerce_script_unit_rebuilds_snapshot_immutable ON commerce_script_unit_rebuilds;
DROP FUNCTION IF EXISTS protect_commerce_script_unit_rebuild_snapshot();
DROP TABLE IF EXISTS commerce_script_unit_rebuilds;

ALTER TABLE commerce_storyboard_plans
    DROP CONSTRAINT IF EXISTS commerce_storyboard_plans_sales_script_contract_fk,
    DROP CONSTRAINT IF EXISTS commerce_storyboard_plans_sales_script_contract_hash_check,
    DROP COLUMN IF EXISTS sales_script_contract_hash,
    DROP COLUMN IF EXISTS sales_script_contract_id;

DROP TRIGGER IF EXISTS commerce_sales_script_contracts_immutable ON commerce_sales_script_contracts;
DROP FUNCTION IF EXISTS protect_commerce_sales_script_contract();
DROP TABLE IF EXISTS commerce_sales_script_contracts;
