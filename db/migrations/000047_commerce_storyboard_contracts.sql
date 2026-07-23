-- +goose Up

SET search_path TO public;

ALTER TABLE commerce_product_reference_pack_items
    ADD CONSTRAINT commerce_pack_items_reference_identity_unique
        UNIQUE(id, reference_pack_id, product_reference_id, organization_id, project_id);

CREATE TABLE commerce_storyboard_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    product_version_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    source_script_version_id UUID NOT NULL,
    localization_id UUID NOT NULL,
    reference_pack_id UUID NOT NULL,
    project_production_generation_id UUID NOT NULL,
    script_unit_generation_id UUID NOT NULL,
    commerce_workflow_binding_id UUID NOT NULL,
    commerce_workflow_binding_revision BIGINT NOT NULL,
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    revision INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'planning',
    active BOOLEAN NOT NULL DEFAULT false,
    stale_state TEXT NOT NULL DEFAULT 'fresh',
    target_language TEXT NOT NULL,
    localized_content_hash TEXT NOT NULL,
    localized_contract_hash TEXT NOT NULL,
    timing_policy_version TEXT NOT NULL,
    target_duration_seconds INTEGER NOT NULL,
    aspect_ratio TEXT NOT NULL,
    timeline_timebase BIGINT NOT NULL,
    fps_numerator INTEGER NOT NULL,
    fps_denominator INTEGER NOT NULL,
    estimated_shot_count INTEGER NOT NULL DEFAULT 0,
    actual_shot_count INTEGER NOT NULL DEFAULT 0,
    planner_prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE SET NULL,
    reviewer_prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE SET NULL,
    planner_provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    reviewer_provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    planner_output JSONB NOT NULL DEFAULT '{}'::jsonb,
    reviewer_output JSONB NOT NULL DEFAULT '{}'::jsonb,
    review_status TEXT NOT NULL DEFAULT 'pending',
    plan_hash TEXT NOT NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ,
    stale_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    CONSTRAINT commerce_storyboard_plans_unit_generation_fk
        FOREIGN KEY (script_unit_generation_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_storyboard_plans_project_generation_fk
        FOREIGN KEY (project_production_generation_id, project_id, organization_id)
        REFERENCES project_video_production_generations(id, project_id, organization_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_storyboard_plans_product_version_fk
        FOREIGN KEY (product_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_versions(id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_storyboard_plans_script_version_fk
        FOREIGN KEY (source_script_version_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_versions(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_storyboard_plans_localization_fk
        FOREIGN KEY (localization_id, script_unit_id, source_script_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_localizations(id, script_unit_id, source_script_version_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_storyboard_plans_reference_pack_fk
        FOREIGN KEY (reference_pack_id, product_id, product_version_id, organization_id, project_id)
        REFERENCES commerce_product_reference_packs(id, product_id, product_version_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_storyboard_plans_binding_fk
        FOREIGN KEY (commerce_workflow_binding_id, project_id, organization_id, commerce_workflow_binding_revision)
        REFERENCES project_commerce_workflow_bindings(id, project_id, organization_id, binding_revision) ON DELETE RESTRICT,
    CONSTRAINT commerce_storyboard_plans_revision_check CHECK (revision > 0),
    CONSTRAINT commerce_storyboard_plans_status_check
        CHECK (status IN ('planning', 'reviewing', 'ready', 'failed', 'stale', 'archived')),
    CONSTRAINT commerce_storyboard_plans_stale_check
        CHECK (stale_state IN ('fresh', 'upstream_changed', 'needs_regeneration')),
    CONSTRAINT commerce_storyboard_plans_active_check
        CHECK (NOT active OR (status = 'ready' AND review_status = 'approved' AND stale_state = 'fresh')),
    CONSTRAINT commerce_storyboard_plans_language_check
        CHECK (target_language ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'),
    CONSTRAINT commerce_storyboard_plans_localized_hash_check
        CHECK (localized_content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_storyboard_plans_contract_hash_check
        CHECK (localized_contract_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_storyboard_plans_timing_policy_check CHECK (trim(timing_policy_version) <> ''),
    CONSTRAINT commerce_storyboard_plans_duration_check CHECK (target_duration_seconds IN (15, 30, 60)),
    CONSTRAINT commerce_storyboard_plans_aspect_check CHECK (trim(aspect_ratio) <> ''),
    CONSTRAINT commerce_storyboard_plans_timebase_check CHECK (timeline_timebase > 0),
    CONSTRAINT commerce_storyboard_plans_fps_check CHECK (
        fps_numerator > 0 AND fps_denominator > 0
        AND (timeline_timebase * fps_denominator) % fps_numerator = 0
    ),
    CONSTRAINT commerce_storyboard_plans_shot_counts_check
        CHECK (estimated_shot_count >= 0 AND actual_shot_count >= 0),
    CONSTRAINT commerce_storyboard_plans_output_check CHECK (
        jsonb_typeof(planner_output) = 'object' AND jsonb_typeof(reviewer_output) = 'object'
    ),
    CONSTRAINT commerce_storyboard_plans_review_check
        CHECK (review_status IN ('pending', 'approved', 'rejected', 'changes_requested')),
    CONSTRAINT commerce_storyboard_plans_hash_check CHECK (plan_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_storyboard_plans_lifecycle_check CHECK (
        (status = 'ready' AND activated_at IS NOT NULL AND stale_at IS NULL AND archived_at IS NULL)
        OR (status = 'stale' AND stale_at IS NOT NULL AND archived_at IS NULL)
        OR (status = 'archived' AND archived_at IS NOT NULL)
        OR (status NOT IN ('ready', 'stale', 'archived') AND activated_at IS NULL AND stale_at IS NULL AND archived_at IS NULL)
    ),
    UNIQUE(id, project_id, project_production_generation_id, organization_id),
    UNIQUE(id, script_unit_id, script_unit_generation_id, organization_id, project_id),
    UNIQUE(script_unit_generation_id, revision)
);

CREATE UNIQUE INDEX commerce_storyboard_plans_one_active
    ON commerce_storyboard_plans(script_unit_generation_id)
    WHERE active;

CREATE INDEX commerce_storyboard_plans_unit_status_idx
    ON commerce_storyboard_plans(script_unit_id, script_unit_generation_id, status, created_at DESC);

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_storyboard_plan_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    frozen_generation RECORD;
BEGIN
    SELECT project_production_generation_id,
           commerce_workflow_binding_id,
           commerce_workflow_binding_revision,
           product_version_id,
           source_script_version_id,
           localization_id,
           reference_pack_id
    INTO frozen_generation
    FROM commerce_script_unit_generations
    WHERE id = NEW.script_unit_generation_id
      AND script_unit_id = NEW.script_unit_id
      AND product_id = NEW.product_id
      AND organization_id = NEW.organization_id
      AND project_id = NEW.project_id;

    IF NOT FOUND
       OR frozen_generation.project_production_generation_id IS DISTINCT FROM NEW.project_production_generation_id
       OR frozen_generation.commerce_workflow_binding_id IS DISTINCT FROM NEW.commerce_workflow_binding_id
       OR frozen_generation.commerce_workflow_binding_revision IS DISTINCT FROM NEW.commerce_workflow_binding_revision
       OR frozen_generation.product_version_id IS DISTINCT FROM NEW.product_version_id
       OR frozen_generation.source_script_version_id IS DISTINCT FROM NEW.source_script_version_id
       OR frozen_generation.localization_id IS DISTINCT FROM NEW.localization_id
       OR frozen_generation.reference_pack_id IS DISTINCT FROM NEW.reference_pack_id THEN
        RAISE EXCEPTION 'commerce storyboard plan does not match the frozen unit generation' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_storyboard_plans_identity
BEFORE INSERT OR UPDATE ON commerce_storyboard_plans
FOR EACH ROW EXECUTE FUNCTION validate_commerce_storyboard_plan_identity();

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_storyboard_plan()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status IN ('ready', 'stale', 'archived') AND (
        NEW.organization_id IS DISTINCT FROM OLD.organization_id
        OR NEW.project_id IS DISTINCT FROM OLD.project_id
        OR NEW.product_id IS DISTINCT FROM OLD.product_id
        OR NEW.product_version_id IS DISTINCT FROM OLD.product_version_id
        OR NEW.script_unit_id IS DISTINCT FROM OLD.script_unit_id
        OR NEW.source_script_version_id IS DISTINCT FROM OLD.source_script_version_id
        OR NEW.localization_id IS DISTINCT FROM OLD.localization_id
        OR NEW.reference_pack_id IS DISTINCT FROM OLD.reference_pack_id
        OR NEW.project_production_generation_id IS DISTINCT FROM OLD.project_production_generation_id
        OR NEW.script_unit_generation_id IS DISTINCT FROM OLD.script_unit_generation_id
        OR NEW.commerce_workflow_binding_id IS DISTINCT FROM OLD.commerce_workflow_binding_id
        OR NEW.commerce_workflow_binding_revision IS DISTINCT FROM OLD.commerce_workflow_binding_revision
        OR NEW.revision IS DISTINCT FROM OLD.revision
        OR NEW.target_language IS DISTINCT FROM OLD.target_language
        OR NEW.localized_content_hash IS DISTINCT FROM OLD.localized_content_hash
        OR NEW.localized_contract_hash IS DISTINCT FROM OLD.localized_contract_hash
        OR NEW.timing_policy_version IS DISTINCT FROM OLD.timing_policy_version
        OR NEW.target_duration_seconds IS DISTINCT FROM OLD.target_duration_seconds
        OR NEW.aspect_ratio IS DISTINCT FROM OLD.aspect_ratio
        OR NEW.timeline_timebase IS DISTINCT FROM OLD.timeline_timebase
        OR NEW.fps_numerator IS DISTINCT FROM OLD.fps_numerator
        OR NEW.fps_denominator IS DISTINCT FROM OLD.fps_denominator
        OR NEW.planner_output IS DISTINCT FROM OLD.planner_output
        OR NEW.reviewer_output IS DISTINCT FROM OLD.reviewer_output
        OR NEW.plan_hash IS DISTINCT FROM OLD.plan_hash
    ) THEN
        RAISE EXCEPTION 'reviewed commerce storyboard plans are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'archived' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'archived commerce storyboard plans are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_storyboard_plans_reviewed_immutable
BEFORE UPDATE ON commerce_storyboard_plans
FOR EACH ROW EXECUTE FUNCTION protect_commerce_storyboard_plan();

ALTER TABLE storyboard_shots
    ADD COLUMN commerce_storyboard_plan_id UUID,
    ADD CONSTRAINT storyboard_shots_commerce_plan_fk
        FOREIGN KEY (commerce_storyboard_plan_id, project_id, production_generation_id, organization_id)
        REFERENCES commerce_storyboard_plans(id, project_id, project_production_generation_id, organization_id) ON DELETE CASCADE,
    ADD CONSTRAINT storyboard_shots_plan_kind_check CHECK (
        (storyboard_plan_id IS NOT NULL AND commerce_storyboard_plan_id IS NULL)
        OR (storyboard_plan_id IS NULL AND commerce_storyboard_plan_id IS NOT NULL)
    ),
    ADD CONSTRAINT storyboard_shots_commerce_source_check CHECK (
        (commerce_storyboard_plan_id IS NULL AND storyboard_source IS DISTINCT FROM 'commerce_script')
        OR (
            commerce_storyboard_plan_id IS NOT NULL
            AND storyboard_source = 'commerce_script'
            AND script_id IS NULL
            AND script_version_id IS NULL
            AND script_episode_id IS NULL
            AND script_scene_id IS NULL
        )
    ),
    ADD CONSTRAINT storyboard_shots_commerce_identity_unique
        UNIQUE(id, commerce_storyboard_plan_id, organization_id, project_id);

CREATE INDEX storyboard_shots_commerce_plan_idx
    ON storyboard_shots(commerce_storyboard_plan_id, shot_index)
    WHERE commerce_storyboard_plan_id IS NOT NULL AND deleted_at IS NULL;

CREATE TABLE commerce_shot_contracts (
    storyboard_shot_id UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    commerce_storyboard_plan_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    script_unit_generation_id UUID NOT NULL,
    sales_beat TEXT NOT NULL,
    visual_action TEXT NOT NULL,
    product_presentation JSONB NOT NULL,
    voiceover_text TEXT NOT NULL DEFAULT '',
    onscreen_text TEXT NOT NULL DEFAULT '',
    target_language TEXT NOT NULL,
    sound_effects JSONB NOT NULL DEFAULT '[]'::jsonb,
    music_cue TEXT NOT NULL DEFAULT '',
    compliance_flags JSONB NOT NULL DEFAULT '[]'::jsonb,
    contract_hash TEXT NOT NULL,
    review_status TEXT NOT NULL DEFAULT 'pending',
    reviewer_output JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_shot_contracts_shot_fk
        FOREIGN KEY (storyboard_shot_id, commerce_storyboard_plan_id, organization_id, project_id)
        REFERENCES storyboard_shots(id, commerce_storyboard_plan_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_shot_contracts_plan_fk
        FOREIGN KEY (commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id, organization_id, project_id)
        REFERENCES commerce_storyboard_plans(id, script_unit_id, script_unit_generation_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_shot_contracts_sales_beat_check CHECK (trim(sales_beat) <> ''),
    CONSTRAINT commerce_shot_contracts_visual_action_check CHECK (trim(visual_action) <> ''),
    CONSTRAINT commerce_shot_contracts_product_check CHECK (jsonb_typeof(product_presentation) = 'object'),
    CONSTRAINT commerce_shot_contracts_language_check
        CHECK (target_language ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'),
    CONSTRAINT commerce_shot_contracts_effects_check CHECK (jsonb_typeof(sound_effects) = 'array'),
    CONSTRAINT commerce_shot_contracts_compliance_check CHECK (jsonb_typeof(compliance_flags) = 'array'),
    CONSTRAINT commerce_shot_contracts_hash_check CHECK (contract_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_shot_contracts_review_check
        CHECK (review_status IN ('pending', 'approved', 'rejected', 'changes_requested')),
    CONSTRAINT commerce_shot_contracts_reviewer_output_check CHECK (jsonb_typeof(reviewer_output) = 'object'),
    UNIQUE(storyboard_shot_id, commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id, organization_id, project_id)
);

CREATE TABLE commerce_shot_segment_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    storyboard_shot_id UUID NOT NULL,
    commerce_storyboard_plan_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    script_unit_generation_id UUID NOT NULL,
    localization_id UUID NOT NULL,
    localization_segment_id UUID NOT NULL,
    usage TEXT NOT NULL,
    ordinal INTEGER NOT NULL,
    verbatim_start INTEGER,
    verbatim_end INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_shot_segment_links_shot_fk
        FOREIGN KEY (storyboard_shot_id, commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id, organization_id, project_id)
        REFERENCES commerce_shot_contracts(storyboard_shot_id, commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_shot_segment_links_segment_fk
        FOREIGN KEY (localization_segment_id, localization_id, script_unit_id, organization_id, project_id)
        REFERENCES commerce_localization_segments(id, localization_id, script_unit_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_shot_segment_links_usage_check
        CHECK (usage IN ('visual', 'voiceover', 'onscreen', 'cta', 'context')),
    CONSTRAINT commerce_shot_segment_links_ordinal_check CHECK (ordinal >= 0),
    CONSTRAINT commerce_shot_segment_links_verbatim_check CHECK (
        (verbatim_start IS NULL AND verbatim_end IS NULL)
        OR (usage = 'voiceover' AND verbatim_start IS NOT NULL AND verbatim_end IS NOT NULL AND verbatim_start >= 0 AND verbatim_end > verbatim_start)
    ),
    UNIQUE(storyboard_shot_id, usage, ordinal),
    UNIQUE(storyboard_shot_id, localization_segment_id, usage)
);

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_shot_segment_link()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    selected_localization UUID;
    selected_text TEXT;
BEGIN
    SELECT plan.localization_id, segment.voiceover_text
    INTO selected_localization, selected_text
    FROM commerce_storyboard_plans plan
    JOIN commerce_localization_segments segment
      ON segment.id = NEW.localization_segment_id
     AND segment.localization_id = NEW.localization_id
     AND segment.script_unit_id = NEW.script_unit_id
     AND segment.organization_id = NEW.organization_id
     AND segment.project_id = NEW.project_id
    WHERE plan.id = NEW.commerce_storyboard_plan_id
      AND plan.script_unit_generation_id = NEW.script_unit_generation_id
      AND plan.organization_id = NEW.organization_id
      AND plan.project_id = NEW.project_id;

    IF selected_localization IS DISTINCT FROM NEW.localization_id THEN
        RAISE EXCEPTION 'shot segment link localization does not match storyboard plan' USING ERRCODE = '23514';
    END IF;
    IF NEW.verbatim_end IS NOT NULL AND NEW.verbatim_end > char_length(selected_text) THEN
        RAISE EXCEPTION 'shot segment verbatim range exceeds localized voiceover' USING ERRCODE = '22001';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_shot_segment_links_identity
BEFORE INSERT OR UPDATE ON commerce_shot_segment_links
FOR EACH ROW EXECUTE FUNCTION validate_commerce_shot_segment_link();

CREATE TABLE commerce_shot_product_references (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    storyboard_shot_id UUID NOT NULL,
    commerce_storyboard_plan_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    script_unit_generation_id UUID NOT NULL,
    product_reference_id UUID NOT NULL,
    source_pack_id UUID NOT NULL,
    source_pack_item_id UUID NOT NULL,
    role TEXT NOT NULL,
    ordinal INTEGER NOT NULL,
    required BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_shot_product_references_shot_fk
        FOREIGN KEY (storyboard_shot_id, commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id, organization_id, project_id)
        REFERENCES commerce_shot_contracts(storyboard_shot_id, commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_shot_product_references_pack_item_fk
        FOREIGN KEY (source_pack_item_id, source_pack_id, product_reference_id, organization_id, project_id)
        REFERENCES commerce_product_reference_pack_items(id, reference_pack_id, product_reference_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_shot_product_references_role_check
        CHECK (role IN ('primary', 'detail', 'logo', 'usage', 'context')),
    CONSTRAINT commerce_shot_product_references_ordinal_check CHECK (ordinal >= 0),
    UNIQUE(storyboard_shot_id, role, ordinal),
    UNIQUE(storyboard_shot_id, product_reference_id, role)
);

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_shot_product_reference()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    selected_pack UUID;
BEGIN
    SELECT reference_pack_id INTO selected_pack
    FROM commerce_storyboard_plans
    WHERE id = NEW.commerce_storyboard_plan_id
      AND script_unit_id = NEW.script_unit_id
      AND script_unit_generation_id = NEW.script_unit_generation_id
      AND organization_id = NEW.organization_id
      AND project_id = NEW.project_id;
    IF selected_pack IS DISTINCT FROM NEW.source_pack_id THEN
        RAISE EXCEPTION 'shot product reference does not belong to the frozen reference pack' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_shot_product_references_identity
BEFORE INSERT OR UPDATE ON commerce_shot_product_references
FOR EACH ROW EXECUTE FUNCTION validate_commerce_shot_product_reference();

ALTER TABLE video_prompt_plans
    ADD COLUMN commerce_script_unit_id UUID,
    ADD COLUMN commerce_script_unit_generation_id UUID,
    ADD COLUMN commerce_product_id UUID,
    ADD COLUMN product_version_id UUID,
    ADD COLUMN localization_id UUID,
    ADD COLUMN product_reference_pack_id UUID,
    ADD COLUMN commerce_workflow_binding_id UUID,
    ADD COLUMN localized_contract_hash TEXT,
    ADD COLUMN target_language TEXT,
    ADD COLUMN verbatim_voiceover_hash TEXT,
    ADD COLUMN timing_policy_version TEXT,
    ADD COLUMN language_capability_snapshot_hash TEXT,
    ADD CONSTRAINT video_prompt_plans_commerce_unit_generation_fk
        FOREIGN KEY (commerce_script_unit_generation_id, commerce_script_unit_id, commerce_product_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT video_prompt_plans_commerce_product_version_fk
        FOREIGN KEY (product_version_id, commerce_product_id, organization_id, project_id)
        REFERENCES commerce_product_versions(id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT video_prompt_plans_commerce_localization_fk
        FOREIGN KEY (localization_id, commerce_script_unit_id, commerce_product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_localizations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT video_prompt_plans_commerce_pack_fk
        FOREIGN KEY (product_reference_pack_id, commerce_product_id, product_version_id, organization_id, project_id)
        REFERENCES commerce_product_reference_packs(id, product_id, product_version_id, organization_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT video_prompt_plans_commerce_binding_fk
        FOREIGN KEY (commerce_workflow_binding_id, project_id, organization_id)
        REFERENCES project_commerce_workflow_bindings(id, project_id, organization_id) ON DELETE RESTRICT,
    ADD CONSTRAINT video_prompt_plans_commerce_presence_check CHECK (
        (
            commerce_script_unit_id IS NULL
            AND commerce_script_unit_generation_id IS NULL
            AND commerce_product_id IS NULL
            AND product_version_id IS NULL
            AND localization_id IS NULL
            AND product_reference_pack_id IS NULL
            AND commerce_workflow_binding_id IS NULL
            AND localized_contract_hash IS NULL
            AND target_language IS NULL
            AND verbatim_voiceover_hash IS NULL
            AND timing_policy_version IS NULL
            AND language_capability_snapshot_hash IS NULL
        )
        OR (
            commerce_script_unit_id IS NOT NULL
            AND commerce_script_unit_generation_id IS NOT NULL
            AND commerce_product_id IS NOT NULL
            AND product_version_id IS NOT NULL
            AND localization_id IS NOT NULL
            AND product_reference_pack_id IS NOT NULL
            AND commerce_workflow_binding_id IS NOT NULL
            AND localized_contract_hash ~ '^[0-9a-f]{64}$'
            AND target_language ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
            AND verbatim_voiceover_hash ~ '^[0-9a-f]{64}$'
            AND trim(timing_policy_version) <> ''
            AND language_capability_snapshot_hash ~ '^[0-9a-f]{64}$'
        )
    );

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_video_prompt_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    selected_kind TEXT;
    frozen_plan RECORD;
BEGIN
    SELECT project_kind INTO selected_kind FROM projects WHERE id = NEW.project_id;
    IF selected_kind = 'narrative' THEN
        IF NEW.commerce_script_unit_generation_id IS NOT NULL THEN
            RAISE EXCEPTION 'narrative video prompt plan cannot carry commerce identity' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    SELECT plan.script_unit_id,
           plan.script_unit_generation_id,
           plan.product_id,
           plan.product_version_id,
           plan.localization_id,
           plan.reference_pack_id,
           plan.commerce_workflow_binding_id,
           plan.localized_contract_hash,
           plan.target_language,
           plan.timing_policy_version
    INTO frozen_plan
    FROM storyboard_shots shot
    JOIN commerce_storyboard_plans plan ON plan.id = shot.commerce_storyboard_plan_id
    WHERE shot.id = NEW.storyboard_shot_id
      AND shot.project_id = NEW.project_id
      AND shot.production_generation_id = NEW.production_generation_id;

    IF NOT FOUND
       OR frozen_plan.script_unit_id IS DISTINCT FROM NEW.commerce_script_unit_id
       OR frozen_plan.script_unit_generation_id IS DISTINCT FROM NEW.commerce_script_unit_generation_id
       OR frozen_plan.product_id IS DISTINCT FROM NEW.commerce_product_id
       OR frozen_plan.product_version_id IS DISTINCT FROM NEW.product_version_id
       OR frozen_plan.localization_id IS DISTINCT FROM NEW.localization_id
       OR frozen_plan.reference_pack_id IS DISTINCT FROM NEW.product_reference_pack_id
       OR frozen_plan.commerce_workflow_binding_id IS DISTINCT FROM NEW.commerce_workflow_binding_id
       OR frozen_plan.localized_contract_hash IS DISTINCT FROM NEW.localized_contract_hash
       OR frozen_plan.target_language IS DISTINCT FROM NEW.target_language
       OR frozen_plan.timing_policy_version IS DISTINCT FROM NEW.timing_policy_version THEN
        RAISE EXCEPTION 'commerce video prompt plan identity does not match storyboard inputs' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER video_prompt_plans_commerce_identity
BEFORE INSERT OR UPDATE OF project_id, production_generation_id, storyboard_shot_id,
    commerce_script_unit_id, commerce_script_unit_generation_id, commerce_product_id,
    product_version_id, localization_id, product_reference_pack_id,
    commerce_workflow_binding_id, localized_contract_hash, target_language,
    verbatim_voiceover_hash, timing_policy_version, language_capability_snapshot_hash
ON video_prompt_plans
FOR EACH ROW EXECUTE FUNCTION validate_commerce_video_prompt_identity();

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_video_prompt_provenance()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.commerce_script_unit_id IS DISTINCT FROM OLD.commerce_script_unit_id
       OR NEW.commerce_script_unit_generation_id IS DISTINCT FROM OLD.commerce_script_unit_generation_id
       OR NEW.commerce_product_id IS DISTINCT FROM OLD.commerce_product_id
       OR NEW.product_version_id IS DISTINCT FROM OLD.product_version_id
       OR NEW.localization_id IS DISTINCT FROM OLD.localization_id
       OR NEW.product_reference_pack_id IS DISTINCT FROM OLD.product_reference_pack_id
       OR NEW.commerce_workflow_binding_id IS DISTINCT FROM OLD.commerce_workflow_binding_id
       OR NEW.localized_contract_hash IS DISTINCT FROM OLD.localized_contract_hash
       OR NEW.target_language IS DISTINCT FROM OLD.target_language
       OR NEW.verbatim_voiceover_hash IS DISTINCT FROM OLD.verbatim_voiceover_hash
       OR NEW.timing_policy_version IS DISTINCT FROM OLD.timing_policy_version
       OR NEW.language_capability_snapshot_hash IS DISTINCT FROM OLD.language_capability_snapshot_hash THEN
        RAISE EXCEPTION 'video prompt commerce provenance is immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER video_prompt_plans_commerce_provenance_immutable
BEFORE UPDATE ON video_prompt_plans
FOR EACH ROW EXECUTE FUNCTION protect_commerce_video_prompt_provenance();

ALTER TABLE video_render_plans
    ADD COLUMN commerce_script_unit_id UUID,
    ADD COLUMN commerce_script_unit_generation_id UUID,
    ADD COLUMN commerce_product_id UUID,
    ADD COLUMN product_version_id UUID,
    ADD COLUMN localization_id UUID,
    ADD COLUMN product_reference_pack_id UUID,
    ADD COLUMN commerce_workflow_binding_id UUID,
    ADD COLUMN localized_contract_hash TEXT,
    ADD COLUMN target_language TEXT,
    ADD COLUMN verbatim_voiceover_hash TEXT,
    ADD COLUMN timing_policy_version TEXT,
    ADD COLUMN language_capability_snapshot_hash TEXT,
    ADD CONSTRAINT video_render_plans_commerce_unit_generation_fk
        FOREIGN KEY (commerce_script_unit_generation_id, commerce_script_unit_id, commerce_product_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT video_render_plans_commerce_product_version_fk
        FOREIGN KEY (product_version_id, commerce_product_id, organization_id, project_id)
        REFERENCES commerce_product_versions(id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT video_render_plans_commerce_localization_fk
        FOREIGN KEY (localization_id, commerce_script_unit_id, commerce_product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_localizations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT video_render_plans_commerce_pack_fk
        FOREIGN KEY (product_reference_pack_id, commerce_product_id, product_version_id, organization_id, project_id)
        REFERENCES commerce_product_reference_packs(id, product_id, product_version_id, organization_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT video_render_plans_commerce_binding_fk
        FOREIGN KEY (commerce_workflow_binding_id, project_id, organization_id)
        REFERENCES project_commerce_workflow_bindings(id, project_id, organization_id) ON DELETE RESTRICT,
    ADD CONSTRAINT video_render_plans_commerce_presence_check CHECK (
        (
            commerce_script_unit_id IS NULL
            AND commerce_script_unit_generation_id IS NULL
            AND commerce_product_id IS NULL
            AND product_version_id IS NULL
            AND localization_id IS NULL
            AND product_reference_pack_id IS NULL
            AND commerce_workflow_binding_id IS NULL
            AND localized_contract_hash IS NULL
            AND target_language IS NULL
            AND verbatim_voiceover_hash IS NULL
            AND timing_policy_version IS NULL
            AND language_capability_snapshot_hash IS NULL
        )
        OR (
            commerce_script_unit_id IS NOT NULL
            AND commerce_script_unit_generation_id IS NOT NULL
            AND commerce_product_id IS NOT NULL
            AND product_version_id IS NOT NULL
            AND localization_id IS NOT NULL
            AND product_reference_pack_id IS NOT NULL
            AND commerce_workflow_binding_id IS NOT NULL
            AND localized_contract_hash ~ '^[0-9a-f]{64}$'
            AND target_language ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
            AND verbatim_voiceover_hash ~ '^[0-9a-f]{64}$'
            AND trim(timing_policy_version) <> ''
            AND language_capability_snapshot_hash ~ '^[0-9a-f]{64}$'
        )
    );

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_video_render_plan_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    selected_kind TEXT;
    prompt_identity RECORD;
BEGIN
    SELECT project_kind INTO selected_kind FROM projects WHERE id = NEW.project_id;
    IF selected_kind = 'narrative' THEN
        IF NEW.commerce_script_unit_generation_id IS NOT NULL THEN
            RAISE EXCEPTION 'narrative render plan cannot carry commerce identity' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    SELECT commerce_script_unit_id,
           commerce_script_unit_generation_id,
           commerce_product_id,
           product_version_id,
           localization_id,
           product_reference_pack_id,
           commerce_workflow_binding_id,
           localized_contract_hash,
           target_language,
           verbatim_voiceover_hash,
           timing_policy_version,
           language_capability_snapshot_hash
    INTO prompt_identity
    FROM video_prompt_plans
    WHERE id = NEW.video_prompt_plan_id
      AND storyboard_shot_id = NEW.storyboard_shot_id
      AND project_id = NEW.project_id
      AND production_generation_id = NEW.production_generation_id
      AND status = 'approved';

    IF NOT FOUND
       OR prompt_identity.commerce_script_unit_id IS DISTINCT FROM NEW.commerce_script_unit_id
       OR prompt_identity.commerce_script_unit_generation_id IS DISTINCT FROM NEW.commerce_script_unit_generation_id
       OR prompt_identity.commerce_product_id IS DISTINCT FROM NEW.commerce_product_id
       OR prompt_identity.product_version_id IS DISTINCT FROM NEW.product_version_id
       OR prompt_identity.localization_id IS DISTINCT FROM NEW.localization_id
       OR prompt_identity.product_reference_pack_id IS DISTINCT FROM NEW.product_reference_pack_id
       OR prompt_identity.commerce_workflow_binding_id IS DISTINCT FROM NEW.commerce_workflow_binding_id
       OR prompt_identity.localized_contract_hash IS DISTINCT FROM NEW.localized_contract_hash
       OR prompt_identity.target_language IS DISTINCT FROM NEW.target_language
       OR prompt_identity.verbatim_voiceover_hash IS DISTINCT FROM NEW.verbatim_voiceover_hash
       OR prompt_identity.timing_policy_version IS DISTINCT FROM NEW.timing_policy_version
       OR prompt_identity.language_capability_snapshot_hash IS DISTINCT FROM NEW.language_capability_snapshot_hash THEN
        RAISE EXCEPTION 'commerce render plan identity does not match approved prompt plan' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER video_render_plans_commerce_identity
BEFORE INSERT OR UPDATE OF project_id, production_generation_id, storyboard_shot_id,
    video_prompt_plan_id, commerce_script_unit_id, commerce_script_unit_generation_id,
    commerce_product_id, product_version_id, localization_id, product_reference_pack_id,
    commerce_workflow_binding_id, localized_contract_hash, target_language,
    verbatim_voiceover_hash, timing_policy_version, language_capability_snapshot_hash
ON video_render_plans
FOR EACH ROW EXECUTE FUNCTION validate_commerce_video_render_plan_identity();

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_video_render_provenance()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.commerce_script_unit_id IS DISTINCT FROM OLD.commerce_script_unit_id
       OR NEW.commerce_script_unit_generation_id IS DISTINCT FROM OLD.commerce_script_unit_generation_id
       OR NEW.commerce_product_id IS DISTINCT FROM OLD.commerce_product_id
       OR NEW.product_version_id IS DISTINCT FROM OLD.product_version_id
       OR NEW.localization_id IS DISTINCT FROM OLD.localization_id
       OR NEW.product_reference_pack_id IS DISTINCT FROM OLD.product_reference_pack_id
       OR NEW.commerce_workflow_binding_id IS DISTINCT FROM OLD.commerce_workflow_binding_id
       OR NEW.localized_contract_hash IS DISTINCT FROM OLD.localized_contract_hash
       OR NEW.target_language IS DISTINCT FROM OLD.target_language
       OR NEW.verbatim_voiceover_hash IS DISTINCT FROM OLD.verbatim_voiceover_hash
       OR NEW.timing_policy_version IS DISTINCT FROM OLD.timing_policy_version
       OR NEW.language_capability_snapshot_hash IS DISTINCT FROM OLD.language_capability_snapshot_hash THEN
        RAISE EXCEPTION 'render plan commerce provenance is immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER video_render_plans_commerce_provenance_immutable
BEFORE UPDATE ON video_render_plans
FOR EACH ROW EXECUTE FUNCTION protect_commerce_video_render_provenance();

CREATE INDEX video_render_plans_commerce_unit_idx
    ON video_render_plans(commerce_script_unit_id, commerce_script_unit_generation_id, status, created_at DESC)
    WHERE commerce_script_unit_generation_id IS NOT NULL;

-- +goose Down

SET search_path TO public;

DROP INDEX IF EXISTS video_render_plans_commerce_unit_idx;
DROP TRIGGER IF EXISTS video_render_plans_commerce_provenance_immutable ON video_render_plans;
DROP FUNCTION IF EXISTS protect_commerce_video_render_provenance();
DROP TRIGGER IF EXISTS video_render_plans_commerce_identity ON video_render_plans;
DROP FUNCTION IF EXISTS validate_commerce_video_render_plan_identity();

ALTER TABLE video_render_plans
    DROP CONSTRAINT IF EXISTS video_render_plans_commerce_presence_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_commerce_binding_fk,
    DROP CONSTRAINT IF EXISTS video_render_plans_commerce_pack_fk,
    DROP CONSTRAINT IF EXISTS video_render_plans_commerce_localization_fk,
    DROP CONSTRAINT IF EXISTS video_render_plans_commerce_product_version_fk,
    DROP CONSTRAINT IF EXISTS video_render_plans_commerce_unit_generation_fk,
    DROP COLUMN IF EXISTS language_capability_snapshot_hash,
    DROP COLUMN IF EXISTS timing_policy_version,
    DROP COLUMN IF EXISTS verbatim_voiceover_hash,
    DROP COLUMN IF EXISTS target_language,
    DROP COLUMN IF EXISTS localized_contract_hash,
    DROP COLUMN IF EXISTS commerce_workflow_binding_id,
    DROP COLUMN IF EXISTS product_reference_pack_id,
    DROP COLUMN IF EXISTS localization_id,
    DROP COLUMN IF EXISTS product_version_id,
    DROP COLUMN IF EXISTS commerce_product_id,
    DROP COLUMN IF EXISTS commerce_script_unit_generation_id,
    DROP COLUMN IF EXISTS commerce_script_unit_id;

DROP TRIGGER IF EXISTS video_prompt_plans_commerce_provenance_immutable ON video_prompt_plans;
DROP FUNCTION IF EXISTS protect_commerce_video_prompt_provenance();
DROP TRIGGER IF EXISTS video_prompt_plans_commerce_identity ON video_prompt_plans;
DROP FUNCTION IF EXISTS validate_commerce_video_prompt_identity();

ALTER TABLE video_prompt_plans
    DROP CONSTRAINT IF EXISTS video_prompt_plans_commerce_presence_check,
    DROP CONSTRAINT IF EXISTS video_prompt_plans_commerce_binding_fk,
    DROP CONSTRAINT IF EXISTS video_prompt_plans_commerce_pack_fk,
    DROP CONSTRAINT IF EXISTS video_prompt_plans_commerce_localization_fk,
    DROP CONSTRAINT IF EXISTS video_prompt_plans_commerce_product_version_fk,
    DROP CONSTRAINT IF EXISTS video_prompt_plans_commerce_unit_generation_fk,
    DROP COLUMN IF EXISTS language_capability_snapshot_hash,
    DROP COLUMN IF EXISTS timing_policy_version,
    DROP COLUMN IF EXISTS verbatim_voiceover_hash,
    DROP COLUMN IF EXISTS target_language,
    DROP COLUMN IF EXISTS localized_contract_hash,
    DROP COLUMN IF EXISTS commerce_workflow_binding_id,
    DROP COLUMN IF EXISTS product_reference_pack_id,
    DROP COLUMN IF EXISTS localization_id,
    DROP COLUMN IF EXISTS product_version_id,
    DROP COLUMN IF EXISTS commerce_product_id,
    DROP COLUMN IF EXISTS commerce_script_unit_generation_id,
    DROP COLUMN IF EXISTS commerce_script_unit_id;

DROP TRIGGER IF EXISTS commerce_shot_product_references_identity ON commerce_shot_product_references;
DROP FUNCTION IF EXISTS validate_commerce_shot_product_reference();
DROP TABLE IF EXISTS commerce_shot_product_references;

DROP TRIGGER IF EXISTS commerce_shot_segment_links_identity ON commerce_shot_segment_links;
DROP FUNCTION IF EXISTS validate_commerce_shot_segment_link();
DROP TABLE IF EXISTS commerce_shot_segment_links;
DROP TABLE IF EXISTS commerce_shot_contracts;

DROP INDEX IF EXISTS storyboard_shots_commerce_plan_idx;
ALTER TABLE storyboard_shots
    DROP CONSTRAINT IF EXISTS storyboard_shots_commerce_identity_unique,
    DROP CONSTRAINT IF EXISTS storyboard_shots_commerce_source_check,
    DROP CONSTRAINT IF EXISTS storyboard_shots_plan_kind_check,
    DROP CONSTRAINT IF EXISTS storyboard_shots_commerce_plan_fk,
    DROP COLUMN IF EXISTS commerce_storyboard_plan_id;

DROP TRIGGER IF EXISTS commerce_storyboard_plans_reviewed_immutable ON commerce_storyboard_plans;
DROP FUNCTION IF EXISTS protect_commerce_storyboard_plan();
DROP TRIGGER IF EXISTS commerce_storyboard_plans_identity ON commerce_storyboard_plans;
DROP FUNCTION IF EXISTS validate_commerce_storyboard_plan_identity();
DROP TABLE IF EXISTS commerce_storyboard_plans;

ALTER TABLE commerce_product_reference_pack_items
    DROP CONSTRAINT IF EXISTS commerce_pack_items_reference_identity_unique;
