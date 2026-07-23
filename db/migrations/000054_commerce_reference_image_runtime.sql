-- +goose Up

CREATE TABLE commerce_image_prompt_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    storyboard_shot_id UUID NOT NULL,
    commerce_storyboard_plan_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    script_unit_generation_id UUID NOT NULL,
    product_id UUID NOT NULL,
    product_version_id UUID NOT NULL,
    localization_id UUID NOT NULL,
    product_reference_pack_id UUID NOT NULL,
    commerce_workflow_binding_id UUID NOT NULL,
    revision INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    active BOOLEAN NOT NULL DEFAULT false,
    prompt TEXT NOT NULL,
    negative_prompt TEXT NOT NULL DEFAULT '',
    reference_snapshot JSONB NOT NULL,
    reference_snapshot_hash TEXT NOT NULL,
    shot_contract_hash TEXT NOT NULL,
    input_hash TEXT NOT NULL,
    prompt_hash TEXT NOT NULL,
    generation_prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE RESTRICT,
    generation_provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    generation_provider_model_id UUID REFERENCES provider_models(id) ON DELETE SET NULL,
    review_prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE RESTRICT,
    review_provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    review_provider_model_id UUID REFERENCES provider_models(id) ON DELETE SET NULL,
    image_provider_model_id UUID REFERENCES provider_models(id) ON DELETE SET NULL,
    review_round INTEGER NOT NULL DEFAULT 1,
    reviewer_output JSONB NOT NULL DEFAULT '{}'::jsonb,
    rejection_reasons JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    approved_at TIMESTAMPTZ,
    superseded_at TIMESTAMPTZ,
    CONSTRAINT commerce_image_prompt_plans_shot_fk
        FOREIGN KEY (storyboard_shot_id, commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id, organization_id, project_id)
        REFERENCES commerce_shot_contracts(storyboard_shot_id, commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_image_prompt_plans_product_version_fk
        FOREIGN KEY (product_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_versions(id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_image_prompt_plans_localization_fk
        FOREIGN KEY (localization_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_localizations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_image_prompt_plans_pack_fk
        FOREIGN KEY (product_reference_pack_id, product_id, product_version_id, organization_id, project_id)
        REFERENCES commerce_product_reference_packs(id, product_id, product_version_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_image_prompt_plans_binding_fk
        FOREIGN KEY (commerce_workflow_binding_id, project_id, organization_id)
        REFERENCES project_commerce_workflow_bindings(id, project_id, organization_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_image_prompt_plans_revision_check CHECK (revision > 0),
    CONSTRAINT commerce_image_prompt_plans_status_check
        CHECK (status IN ('pending', 'approved', 'rejected', 'failed', 'stale')),
    CONSTRAINT commerce_image_prompt_plans_prompt_check CHECK (trim(prompt) <> ''),
    CONSTRAINT commerce_image_prompt_plans_reference_check CHECK (jsonb_typeof(reference_snapshot) = 'array'),
    CONSTRAINT commerce_image_prompt_plans_reviewer_check CHECK (jsonb_typeof(reviewer_output) = 'object'),
    CONSTRAINT commerce_image_prompt_plans_rejections_check CHECK (jsonb_typeof(rejection_reasons) = 'array'),
    CONSTRAINT commerce_image_prompt_plans_hashes_check CHECK (
        reference_snapshot_hash ~ '^[0-9a-f]{64}$'
        AND shot_contract_hash ~ '^[0-9a-f]{64}$'
        AND input_hash ~ '^[0-9a-f]{64}$'
        AND prompt_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT commerce_image_prompt_plans_round_check CHECK (review_round BETWEEN 1 AND 3),
    CONSTRAINT commerce_image_prompt_plans_approval_check CHECK (
        (status = 'approved' AND approved_at IS NOT NULL)
        OR (status <> 'approved' AND active = false)
    ),
    UNIQUE(storyboard_shot_id, revision),
    UNIQUE(id, storyboard_shot_id, script_unit_generation_id, organization_id, project_id)
);

CREATE UNIQUE INDEX commerce_image_prompt_plans_one_active_idx
    ON commerce_image_prompt_plans(storyboard_shot_id)
    WHERE active;

CREATE INDEX commerce_image_prompt_plans_unit_status_idx
    ON commerce_image_prompt_plans(script_unit_generation_id, status, created_at DESC);

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_image_prompt_plan_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    frozen_plan RECORD;
BEGIN
    SELECT plan.product_id,
           plan.product_version_id,
           plan.localization_id,
           plan.reference_pack_id,
           plan.commerce_workflow_binding_id,
           contract.contract_hash
    INTO frozen_plan
    FROM commerce_storyboard_plans plan
    JOIN commerce_shot_contracts contract
      ON contract.commerce_storyboard_plan_id = plan.id
     AND contract.storyboard_shot_id = NEW.storyboard_shot_id
    WHERE plan.id = NEW.commerce_storyboard_plan_id
      AND plan.script_unit_id = NEW.script_unit_id
      AND plan.script_unit_generation_id = NEW.script_unit_generation_id
      AND plan.organization_id = NEW.organization_id
      AND plan.project_id = NEW.project_id;

    IF NOT FOUND
       OR frozen_plan.product_id IS DISTINCT FROM NEW.product_id
       OR frozen_plan.product_version_id IS DISTINCT FROM NEW.product_version_id
       OR frozen_plan.localization_id IS DISTINCT FROM NEW.localization_id
       OR frozen_plan.reference_pack_id IS DISTINCT FROM NEW.product_reference_pack_id
       OR frozen_plan.commerce_workflow_binding_id IS DISTINCT FROM NEW.commerce_workflow_binding_id
       OR frozen_plan.contract_hash IS DISTINCT FROM NEW.shot_contract_hash THEN
        RAISE EXCEPTION 'commerce image prompt plan identity does not match frozen storyboard inputs' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_image_prompt_plans_identity
BEFORE INSERT OR UPDATE OF organization_id, project_id, storyboard_shot_id,
    commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id,
    product_id, product_version_id, localization_id, product_reference_pack_id,
    commerce_workflow_binding_id, shot_contract_hash
ON commerce_image_prompt_plans
FOR EACH ROW EXECUTE FUNCTION validate_commerce_image_prompt_plan_identity();

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_image_prompt_plan()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status IN ('approved', 'stale') AND (
        NEW.organization_id IS DISTINCT FROM OLD.organization_id
        OR NEW.project_id IS DISTINCT FROM OLD.project_id
        OR NEW.storyboard_shot_id IS DISTINCT FROM OLD.storyboard_shot_id
        OR NEW.commerce_storyboard_plan_id IS DISTINCT FROM OLD.commerce_storyboard_plan_id
        OR NEW.script_unit_id IS DISTINCT FROM OLD.script_unit_id
        OR NEW.script_unit_generation_id IS DISTINCT FROM OLD.script_unit_generation_id
        OR NEW.product_id IS DISTINCT FROM OLD.product_id
        OR NEW.product_version_id IS DISTINCT FROM OLD.product_version_id
        OR NEW.localization_id IS DISTINCT FROM OLD.localization_id
        OR NEW.product_reference_pack_id IS DISTINCT FROM OLD.product_reference_pack_id
        OR NEW.commerce_workflow_binding_id IS DISTINCT FROM OLD.commerce_workflow_binding_id
        OR NEW.revision IS DISTINCT FROM OLD.revision
        OR NEW.prompt IS DISTINCT FROM OLD.prompt
        OR NEW.negative_prompt IS DISTINCT FROM OLD.negative_prompt
        OR NEW.reference_snapshot IS DISTINCT FROM OLD.reference_snapshot
        OR NEW.reference_snapshot_hash IS DISTINCT FROM OLD.reference_snapshot_hash
        OR NEW.shot_contract_hash IS DISTINCT FROM OLD.shot_contract_hash
        OR NEW.input_hash IS DISTINCT FROM OLD.input_hash
        OR NEW.prompt_hash IS DISTINCT FROM OLD.prompt_hash
        OR NEW.generation_prompt_version_id IS DISTINCT FROM OLD.generation_prompt_version_id
        OR NEW.review_prompt_version_id IS DISTINCT FROM OLD.review_prompt_version_id
        OR NEW.review_round IS DISTINCT FROM OLD.review_round
        OR NEW.reviewer_output IS DISTINCT FROM OLD.reviewer_output
        OR NEW.rejection_reasons IS DISTINCT FROM OLD.rejection_reasons
    ) THEN
        RAISE EXCEPTION 'approved commerce image prompt plan is immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_image_prompt_plans_immutable
BEFORE UPDATE ON commerce_image_prompt_plans
FOR EACH ROW EXECUTE FUNCTION protect_commerce_image_prompt_plan();

CREATE TABLE commerce_shot_image_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    storyboard_shot_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    script_unit_generation_id UUID NOT NULL,
    image_prompt_plan_id UUID NOT NULL,
    revision INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    active BOOLEAN NOT NULL DEFAULT false,
    input_hash TEXT NOT NULL,
    reference_snapshot_hash TEXT NOT NULL,
    provider_request_id UUID REFERENCES provider_requests(id) ON DELETE SET NULL,
    provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    provider_model_id UUID REFERENCES provider_models(id) ON DELETE SET NULL,
    artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
    media_file_id UUID REFERENCES media_files(id) ON DELETE SET NULL,
    storage_key TEXT,
    fidelity_status TEXT NOT NULL DEFAULT 'pending',
    error_code TEXT,
    error_message TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    superseded_at TIMESTAMPTZ,
    CONSTRAINT commerce_shot_image_versions_prompt_fk
        FOREIGN KEY (image_prompt_plan_id, storyboard_shot_id, script_unit_generation_id, organization_id, project_id)
        REFERENCES commerce_image_prompt_plans(id, storyboard_shot_id, script_unit_generation_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_shot_image_versions_shot_fk
        FOREIGN KEY (storyboard_shot_id)
        REFERENCES storyboard_shots(id) ON DELETE CASCADE,
    CONSTRAINT commerce_shot_image_versions_revision_check CHECK (revision > 0),
    CONSTRAINT commerce_shot_image_versions_status_check
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'fidelity_rejected', 'stale', 'cancelled')),
    CONSTRAINT commerce_shot_image_versions_fidelity_check
        CHECK (fidelity_status IN ('pending', 'approved', 'rejected', 'not_reviewed')),
    CONSTRAINT commerce_shot_image_versions_hashes_check CHECK (
        input_hash ~ '^[0-9a-f]{64}$' AND reference_snapshot_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT commerce_shot_image_versions_terminal_check CHECK (
        (status = 'succeeded' AND artifact_id IS NOT NULL AND media_file_id IS NOT NULL AND storage_key IS NOT NULL AND trim(storage_key) <> '' AND completed_at IS NOT NULL AND fidelity_status = 'approved')
        OR (status IN ('failed', 'fidelity_rejected') AND completed_at IS NOT NULL AND error_code IS NOT NULL AND trim(error_code) <> '')
        OR status IN ('queued', 'running', 'stale', 'cancelled')
    ),
    CONSTRAINT commerce_shot_image_versions_active_check CHECK (NOT active OR status = 'succeeded'),
    CONSTRAINT commerce_shot_image_versions_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    UNIQUE(storyboard_shot_id, revision),
    UNIQUE(id, storyboard_shot_id, script_unit_generation_id, organization_id, project_id)
);

CREATE UNIQUE INDEX commerce_shot_image_versions_one_active_idx
    ON commerce_shot_image_versions(storyboard_shot_id)
    WHERE active;

CREATE INDEX commerce_shot_image_versions_unit_status_idx
    ON commerce_shot_image_versions(script_unit_generation_id, status, created_at DESC);

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_shot_image_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.organization_id IS DISTINCT FROM OLD.organization_id
       OR NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.storyboard_shot_id IS DISTINCT FROM OLD.storyboard_shot_id
       OR NEW.script_unit_id IS DISTINCT FROM OLD.script_unit_id
       OR NEW.script_unit_generation_id IS DISTINCT FROM OLD.script_unit_generation_id
       OR NEW.image_prompt_plan_id IS DISTINCT FROM OLD.image_prompt_plan_id
       OR NEW.revision IS DISTINCT FROM OLD.revision
       OR NEW.input_hash IS DISTINCT FROM OLD.input_hash
       OR NEW.reference_snapshot_hash IS DISTINCT FROM OLD.reference_snapshot_hash
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'commerce shot image version identity is immutable' USING ERRCODE = '55000';
    END IF;

    IF OLD.status IN ('succeeded', 'failed', 'fidelity_rejected', 'stale', 'cancelled') THEN
        IF OLD.status = 'succeeded'
           AND NEW.status = 'stale'
           AND NEW.active = false
           AND NEW.superseded_at IS NOT NULL
           AND NEW.provider_request_id IS NOT DISTINCT FROM OLD.provider_request_id
           AND NEW.provider_call_id IS NOT DISTINCT FROM OLD.provider_call_id
           AND NEW.provider_model_id IS NOT DISTINCT FROM OLD.provider_model_id
           AND NEW.artifact_id IS NOT DISTINCT FROM OLD.artifact_id
           AND NEW.media_file_id IS NOT DISTINCT FROM OLD.media_file_id
           AND NEW.storage_key IS NOT DISTINCT FROM OLD.storage_key
           AND NEW.fidelity_status IS NOT DISTINCT FROM OLD.fidelity_status
           AND NEW.error_code IS NOT DISTINCT FROM OLD.error_code
           AND NEW.error_message IS NOT DISTINCT FROM OLD.error_message
           AND NEW.metadata IS NOT DISTINCT FROM OLD.metadata
           AND NEW.started_at IS NOT DISTINCT FROM OLD.started_at
           AND NEW.completed_at IS NOT DISTINCT FROM OLD.completed_at THEN
            RETURN NEW;
        END IF;
        IF NEW IS DISTINCT FROM OLD THEN
            RAISE EXCEPTION 'terminal commerce shot image version is immutable' USING ERRCODE = '55000';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_shot_image_versions_immutable
BEFORE UPDATE ON commerce_shot_image_versions
FOR EACH ROW EXECUTE FUNCTION protect_commerce_shot_image_version();

CREATE TABLE commerce_product_fidelity_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    storyboard_shot_id UUID NOT NULL,
    script_unit_generation_id UUID NOT NULL,
    shot_image_version_id UUID NOT NULL,
    image_prompt_plan_id UUID NOT NULL,
    review_round INTEGER NOT NULL,
    status TEXT NOT NULL,
    score NUMERIC(5,4),
    issues JSONB NOT NULL DEFAULT '[]'::jsonb,
    reviewer_output JSONB NOT NULL DEFAULT '{}'::jsonb,
    prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE RESTRICT,
    provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    provider_model_id UUID REFERENCES provider_models(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_product_fidelity_reviews_image_fk
        FOREIGN KEY (shot_image_version_id, storyboard_shot_id, script_unit_generation_id, organization_id, project_id)
        REFERENCES commerce_shot_image_versions(id, storyboard_shot_id, script_unit_generation_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_product_fidelity_reviews_plan_fk
        FOREIGN KEY (image_prompt_plan_id, storyboard_shot_id, script_unit_generation_id, organization_id, project_id)
        REFERENCES commerce_image_prompt_plans(id, storyboard_shot_id, script_unit_generation_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_product_fidelity_reviews_round_check CHECK (review_round BETWEEN 1 AND 3),
    CONSTRAINT commerce_product_fidelity_reviews_status_check CHECK (status IN ('approved', 'rejected')),
    CONSTRAINT commerce_product_fidelity_reviews_score_check CHECK (score IS NULL OR (score >= 0 AND score <= 1)),
    CONSTRAINT commerce_product_fidelity_reviews_issues_check CHECK (jsonb_typeof(issues) = 'array'),
    CONSTRAINT commerce_product_fidelity_reviews_output_check CHECK (jsonb_typeof(reviewer_output) = 'object'),
    UNIQUE(shot_image_version_id, review_round)
);

ALTER TABLE storyboard_shots
    ADD COLUMN active_commerce_image_prompt_plan_id UUID,
    ADD COLUMN active_commerce_image_version_id UUID,
    ADD CONSTRAINT storyboard_shots_active_commerce_image_prompt_fk
        FOREIGN KEY (active_commerce_image_prompt_plan_id)
        REFERENCES commerce_image_prompt_plans(id) ON DELETE SET NULL,
    ADD CONSTRAINT storyboard_shots_active_commerce_image_version_fk
        FOREIGN KEY (active_commerce_image_version_id)
        REFERENCES commerce_shot_image_versions(id) ON DELETE SET NULL,
    ADD CONSTRAINT storyboard_shots_commerce_image_pointer_check CHECK (
        commerce_storyboard_plan_id IS NOT NULL
        OR (active_commerce_image_prompt_plan_id IS NULL AND active_commerce_image_version_id IS NULL)
    );

-- +goose Down

ALTER TABLE storyboard_shots
    DROP CONSTRAINT IF EXISTS storyboard_shots_commerce_image_pointer_check,
    DROP CONSTRAINT IF EXISTS storyboard_shots_active_commerce_image_version_fk,
    DROP CONSTRAINT IF EXISTS storyboard_shots_active_commerce_image_prompt_fk,
    DROP COLUMN IF EXISTS active_commerce_image_version_id,
    DROP COLUMN IF EXISTS active_commerce_image_prompt_plan_id;

DROP TABLE IF EXISTS commerce_product_fidelity_reviews;
DROP TRIGGER IF EXISTS commerce_shot_image_versions_immutable ON commerce_shot_image_versions;
DROP FUNCTION IF EXISTS protect_commerce_shot_image_version();
DROP INDEX IF EXISTS commerce_shot_image_versions_unit_status_idx;
DROP INDEX IF EXISTS commerce_shot_image_versions_one_active_idx;
DROP TABLE IF EXISTS commerce_shot_image_versions;
DROP TRIGGER IF EXISTS commerce_image_prompt_plans_immutable ON commerce_image_prompt_plans;
DROP FUNCTION IF EXISTS protect_commerce_image_prompt_plan();
DROP TRIGGER IF EXISTS commerce_image_prompt_plans_identity ON commerce_image_prompt_plans;
DROP FUNCTION IF EXISTS validate_commerce_image_prompt_plan_identity();
DROP INDEX IF EXISTS commerce_image_prompt_plans_unit_status_idx;
DROP INDEX IF EXISTS commerce_image_prompt_plans_one_active_idx;
DROP TABLE IF EXISTS commerce_image_prompt_plans;
