-- +goose Up

SET search_path TO public;

ALTER TABLE artifacts
    ADD CONSTRAINT artifacts_id_organization_project_unique
        UNIQUE(id, organization_id, project_id);

ALTER TABLE media_files
    ADD CONSTRAINT media_files_id_organization_project_unique
        UNIQUE(id, organization_id, project_id);

ALTER TABLE project_commerce_workflow_bindings
    ADD CONSTRAINT commerce_bindings_identity_revision_unique
        UNIQUE(id, project_id, organization_id, binding_revision);

CREATE TABLE commerce_products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    current_version_id UUID,
    status TEXT NOT NULL DEFAULT 'draft',
    revision BIGINT NOT NULL DEFAULT 1,
    script_units_revision BIGINT NOT NULL DEFAULT 1,
    next_script_unit_no BIGINT NOT NULL DEFAULT 1,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_products_project_fk
        FOREIGN KEY (project_id, organization_id)
        REFERENCES projects(id, organization_id) ON DELETE CASCADE,
    CONSTRAINT commerce_products_status_check CHECK (status IN ('draft', 'ready', 'archived')),
    CONSTRAINT commerce_products_revision_check CHECK (revision > 0 AND script_units_revision > 0),
    CONSTRAINT commerce_products_next_unit_check CHECK (next_script_unit_no > 0),
    CONSTRAINT commerce_products_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT commerce_products_ready_check CHECK (status <> 'ready' OR current_version_id IS NOT NULL),
    UNIQUE(project_id),
    UNIQUE(id, organization_id, project_id)
);

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_product_project_kind()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM projects
        WHERE id = NEW.project_id
          AND organization_id = NEW.organization_id
          AND project_kind = 'commerce_video'
    ) THEN
        RAISE EXCEPTION 'commerce product requires a commerce project' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_products_project_kind
BEFORE INSERT OR UPDATE OF organization_id, project_id ON commerce_products
FOR EACH ROW EXECUTE FUNCTION validate_commerce_product_project_kind();

CREATE TABLE commerce_product_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    version INTEGER NOT NULL,
    name TEXT NOT NULL,
    brand TEXT NOT NULL DEFAULT '',
    selling_points JSONB NOT NULL DEFAULT '[]'::jsonb,
    immutable_features JSONB NOT NULL DEFAULT '{}'::jsonb,
    prohibited_claims JSONB NOT NULL DEFAULT '[]'::jsonb,
    facts_snapshot JSONB NOT NULL,
    facts_hash TEXT NOT NULL,
    source_version_id UUID,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_product_versions_product_fk
        FOREIGN KEY (product_id, organization_id, project_id)
        REFERENCES commerce_products(id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_product_versions_version_check CHECK (version > 0),
    CONSTRAINT commerce_product_versions_name_check CHECK (trim(name) <> ''),
    CONSTRAINT commerce_product_versions_selling_points_check CHECK (jsonb_typeof(selling_points) = 'array'),
    CONSTRAINT commerce_product_versions_features_check CHECK (jsonb_typeof(immutable_features) = 'object'),
    CONSTRAINT commerce_product_versions_claims_check CHECK (jsonb_typeof(prohibited_claims) = 'array'),
    CONSTRAINT commerce_product_versions_facts_check CHECK (jsonb_typeof(facts_snapshot) = 'object'),
    CONSTRAINT commerce_product_versions_hash_check CHECK (facts_hash ~ '^[0-9a-f]{64}$'),
    UNIQUE(product_id, version),
    UNIQUE(id, product_id, organization_id, project_id)
);

ALTER TABLE commerce_product_versions
    ADD CONSTRAINT commerce_product_versions_source_fk
        FOREIGN KEY (source_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_versions(id, product_id, organization_id, project_id) ON DELETE RESTRICT;

ALTER TABLE commerce_products
    ADD CONSTRAINT commerce_products_current_version_fk
        FOREIGN KEY (current_version_id, id, organization_id, project_id)
        REFERENCES commerce_product_versions(id, product_id, organization_id, project_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY IMMEDIATE;

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_product_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'commerce product versions are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_product_versions_immutable
BEFORE UPDATE ON commerce_product_versions
FOR EACH ROW EXECUTE FUNCTION protect_commerce_product_version();

CREATE TABLE commerce_product_references (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    artifact_id UUID NOT NULL,
    media_file_id UUID NOT NULL,
    reference_role TEXT NOT NULL DEFAULT 'other',
    ordinal INTEGER NOT NULL,
    is_primary BOOLEAN NOT NULL DEFAULT false,
    status TEXT NOT NULL DEFAULT 'active',
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    mime_type TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    quality_review JSONB NOT NULL DEFAULT '{}'::jsonb,
    revision BIGINT NOT NULL DEFAULT 1,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ,
    CONSTRAINT commerce_product_references_product_fk
        FOREIGN KEY (product_id, organization_id, project_id)
        REFERENCES commerce_products(id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_product_references_artifact_fk
        FOREIGN KEY (artifact_id, organization_id, project_id)
        REFERENCES artifacts(id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_product_references_media_fk
        FOREIGN KEY (media_file_id, organization_id, project_id)
        REFERENCES media_files(id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_product_references_role_check
        CHECK (reference_role IN ('primary', 'front', 'back', 'detail', 'usage', 'logo', 'other')),
    CONSTRAINT commerce_product_references_status_check CHECK (status IN ('active', 'archived')),
    CONSTRAINT commerce_product_references_ordinal_check CHECK (ordinal >= 0),
    CONSTRAINT commerce_product_references_dimensions_check CHECK (width > 0 AND height > 0),
    CONSTRAINT commerce_product_references_mime_check CHECK (mime_type LIKE 'image/%'),
    CONSTRAINT commerce_product_references_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_product_references_review_check CHECK (jsonb_typeof(quality_review) = 'object'),
    CONSTRAINT commerce_product_references_revision_check CHECK (revision > 0),
    CONSTRAINT commerce_product_references_archive_check CHECK (
        (status = 'active' AND archived_at IS NULL)
        OR (status = 'archived' AND archived_at IS NOT NULL)
    ),
    UNIQUE(id, product_id, organization_id, project_id)
);

CREATE UNIQUE INDEX commerce_product_references_one_primary
    ON commerce_product_references(product_id)
    WHERE status = 'active' AND is_primary;

CREATE UNIQUE INDEX commerce_product_references_active_hash_unique
    ON commerce_product_references(product_id, content_hash)
    WHERE status = 'active';

CREATE UNIQUE INDEX commerce_product_references_active_ordinal_unique
    ON commerce_product_references(product_id, ordinal)
    WHERE status = 'active';

CREATE TABLE commerce_product_reference_uploads (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    setup_session_id UUID,
    storage_key TEXT NOT NULL UNIQUE,
    requested_mime_type TEXT NOT NULL,
    original_file_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    idempotency_key TEXT NOT NULL,
    reference_id UUID,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    abandoned_at TIMESTAMPTZ,
    CONSTRAINT commerce_product_reference_uploads_product_fk
        FOREIGN KEY (product_id, organization_id, project_id)
        REFERENCES commerce_products(id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_product_reference_uploads_setup_fk
        FOREIGN KEY (setup_session_id, organization_id)
        REFERENCES commerce_setup_sessions(id, organization_id) ON DELETE CASCADE,
    CONSTRAINT commerce_product_reference_uploads_reference_fk
        FOREIGN KEY (reference_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_references(id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_product_reference_uploads_mime_check
        CHECK (requested_mime_type IN ('image/jpeg', 'image/png', 'image/webp')),
    CONSTRAINT commerce_product_reference_uploads_status_check
        CHECK (status IN ('pending', 'completed', 'abandoned', 'expired')),
    CONSTRAINT commerce_product_reference_uploads_lifecycle_check CHECK (
        (status = 'pending' AND reference_id IS NULL AND completed_at IS NULL AND abandoned_at IS NULL)
        OR (status = 'completed' AND reference_id IS NOT NULL AND completed_at IS NOT NULL AND abandoned_at IS NULL)
        OR (status IN ('abandoned', 'expired') AND reference_id IS NULL AND completed_at IS NULL AND abandoned_at IS NOT NULL)
    ),
    UNIQUE(organization_id, idempotency_key)
);

CREATE INDEX commerce_product_reference_uploads_cleanup_idx
    ON commerce_product_reference_uploads(status, expires_at)
    WHERE status = 'pending';

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_product_reference_media()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    selected_artifact UUID;
    selected_mime TEXT;
BEGIN
    SELECT artifact_id, mime_type
    INTO selected_artifact, selected_mime
    FROM media_files
    WHERE id = NEW.media_file_id
      AND organization_id = NEW.organization_id
      AND project_id = NEW.project_id;

    IF selected_artifact IS DISTINCT FROM NEW.artifact_id THEN
        RAISE EXCEPTION 'product reference media must belong to the selected artifact' USING ERRCODE = '23514';
    END IF;
    IF selected_mime IS DISTINCT FROM NEW.mime_type OR selected_mime NOT LIKE 'image/%' THEN
        RAISE EXCEPTION 'product reference media facts do not match the stored media' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_product_references_media_identity
BEFORE INSERT OR UPDATE OF organization_id, project_id, artifact_id, media_file_id, mime_type
ON commerce_product_references
FOR EACH ROW EXECUTE FUNCTION validate_commerce_product_reference_media();

CREATE TABLE commerce_product_reference_packs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    product_version_id UUID NOT NULL,
    product_facts_hash TEXT NOT NULL,
    reference_set_hash TEXT NOT NULL,
    pack_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    stale_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    CONSTRAINT commerce_reference_packs_product_version_fk
        FOREIGN KEY (product_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_versions(id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_reference_packs_status_check CHECK (status IN ('active', 'stale', 'archived')),
    CONSTRAINT commerce_reference_packs_product_hash_check CHECK (product_facts_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_reference_packs_set_hash_check CHECK (reference_set_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_reference_packs_pack_hash_check CHECK (pack_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_reference_packs_lifecycle_check CHECK (
        (status = 'active' AND stale_at IS NULL AND archived_at IS NULL)
        OR (status = 'stale' AND stale_at IS NOT NULL AND archived_at IS NULL)
        OR (status = 'archived' AND archived_at IS NOT NULL)
    ),
    UNIQUE(product_id, pack_hash),
    UNIQUE(id, product_id, organization_id, project_id),
    UNIQUE(id, product_id, product_version_id, organization_id, project_id)
);

CREATE TABLE commerce_product_reference_pack_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    product_version_id UUID NOT NULL,
    reference_pack_id UUID NOT NULL,
    product_reference_id UUID NOT NULL,
    ordinal INTEGER NOT NULL,
    reference_role TEXT NOT NULL,
    artifact_id UUID NOT NULL,
    media_file_id UUID NOT NULL,
    content_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_reference_pack_items_pack_fk
        FOREIGN KEY (reference_pack_id, product_id, product_version_id, organization_id, project_id)
        REFERENCES commerce_product_reference_packs(id, product_id, product_version_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_reference_pack_items_reference_fk
        FOREIGN KEY (product_reference_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_references(id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_reference_pack_items_artifact_fk
        FOREIGN KEY (artifact_id, organization_id, project_id)
        REFERENCES artifacts(id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_reference_pack_items_media_fk
        FOREIGN KEY (media_file_id, organization_id, project_id)
        REFERENCES media_files(id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_reference_pack_items_ordinal_check CHECK (ordinal >= 0),
    CONSTRAINT commerce_reference_pack_items_role_check
        CHECK (reference_role IN ('primary', 'front', 'back', 'detail', 'usage', 'logo', 'other')),
    CONSTRAINT commerce_reference_pack_items_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    UNIQUE(reference_pack_id, ordinal),
    UNIQUE(reference_pack_id, product_reference_id),
    UNIQUE(id, reference_pack_id, organization_id, project_id)
);

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_reference_pack_snapshot()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    selected_facts_hash TEXT;
    selected_role TEXT;
    selected_artifact UUID;
    selected_media UUID;
    selected_content_hash TEXT;
BEGIN
    IF TG_TABLE_NAME = 'commerce_product_reference_packs' THEN
        SELECT facts_hash INTO selected_facts_hash
        FROM commerce_product_versions
        WHERE id = NEW.product_version_id
          AND product_id = NEW.product_id
          AND organization_id = NEW.organization_id
          AND project_id = NEW.project_id;
        IF selected_facts_hash IS DISTINCT FROM NEW.product_facts_hash THEN
            RAISE EXCEPTION 'reference pack product facts hash mismatch' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    SELECT reference_role, artifact_id, media_file_id, content_hash
    INTO selected_role, selected_artifact, selected_media, selected_content_hash
    FROM commerce_product_references
    WHERE id = NEW.product_reference_id
      AND product_id = NEW.product_id
      AND organization_id = NEW.organization_id
      AND project_id = NEW.project_id
      AND status = 'active';
    IF NOT FOUND
       OR selected_role IS DISTINCT FROM NEW.reference_role
       OR selected_artifact IS DISTINCT FROM NEW.artifact_id
       OR selected_media IS DISTINCT FROM NEW.media_file_id
       OR selected_content_hash IS DISTINCT FROM NEW.content_hash THEN
        RAISE EXCEPTION 'reference pack item snapshot mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_reference_packs_snapshot
BEFORE INSERT ON commerce_product_reference_packs
FOR EACH ROW EXECUTE FUNCTION validate_commerce_reference_pack_snapshot();

CREATE TRIGGER commerce_reference_pack_items_snapshot
BEFORE INSERT ON commerce_product_reference_pack_items
FOR EACH ROW EXECUTE FUNCTION validate_commerce_reference_pack_snapshot();

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_reference_pack()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.organization_id IS DISTINCT FROM OLD.organization_id
       OR NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.product_id IS DISTINCT FROM OLD.product_id
       OR NEW.product_version_id IS DISTINCT FROM OLD.product_version_id
       OR NEW.product_facts_hash IS DISTINCT FROM OLD.product_facts_hash
       OR NEW.reference_set_hash IS DISTINCT FROM OLD.reference_set_hash
       OR NEW.pack_hash IS DISTINCT FROM OLD.pack_hash
       OR NEW.workflow_run_id IS DISTINCT FROM OLD.workflow_run_id
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'commerce reference packs are immutable snapshots' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'archived' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'archived commerce reference packs are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'stale' AND NEW.status NOT IN ('stale', 'archived') THEN
        RAISE EXCEPTION 'stale reference packs cannot be reactivated' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'active' AND NEW.status NOT IN ('active', 'stale', 'archived') THEN
        RAISE EXCEPTION 'invalid reference pack transition' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_reference_packs_immutable
BEFORE UPDATE ON commerce_product_reference_packs
FOR EACH ROW EXECUTE FUNCTION protect_commerce_reference_pack();

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_reference_pack_item()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'commerce reference pack items are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_reference_pack_items_immutable
BEFORE UPDATE ON commerce_product_reference_pack_items
FOR EACH ROW EXECUTE FUNCTION protect_commerce_reference_pack_item();

CREATE TABLE commerce_script_units (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    unit_no BIGINT NOT NULL,
    title TEXT NOT NULL,
    sort_order BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    current_source_version_id UUID,
    current_localization_id UUID,
    language_mode TEXT NOT NULL DEFAULT 'auto',
    explicit_target_language TEXT,
    target_duration_seconds INTEGER NOT NULL DEFAULT 30,
    target_platform TEXT NOT NULL DEFAULT 'generic',
    draft_content TEXT NOT NULL DEFAULT '',
    draft_content_hash TEXT,
    draft_updated_at TIMESTAMPTZ,
    active_unit_generation_id UUID,
    unit_generation_no BIGINT NOT NULL DEFAULT 0,
    derived_from_script_unit_id UUID,
    derivation_kind TEXT,
    revision BIGINT NOT NULL DEFAULT 1,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ,
    CONSTRAINT commerce_script_units_product_fk
        FOREIGN KEY (product_id, organization_id, project_id)
        REFERENCES commerce_products(id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_script_units_number_check CHECK (unit_no > 0),
    CONSTRAINT commerce_script_units_title_check CHECK (trim(title) <> ''),
    CONSTRAINT commerce_script_units_sort_check CHECK (sort_order > 0),
    CONSTRAINT commerce_script_units_status_check CHECK (status IN ('draft', 'ready', 'archived')),
    CONSTRAINT commerce_script_units_language_mode_check CHECK (language_mode IN ('auto', 'explicit')),
    CONSTRAINT commerce_script_units_language_check CHECK (
        (language_mode = 'auto' AND explicit_target_language IS NULL)
        OR (
            language_mode = 'explicit'
            AND explicit_target_language ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
        )
    ),
    CONSTRAINT commerce_script_units_duration_check CHECK (target_duration_seconds IN (15, 30, 60)),
    CONSTRAINT commerce_script_units_platform_check CHECK (trim(target_platform) <> ''),
    CONSTRAINT commerce_script_units_draft_hash_check CHECK (
        draft_content_hash IS NULL OR draft_content_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT commerce_script_units_generation_number_check CHECK (unit_generation_no >= 0),
    CONSTRAINT commerce_script_units_derivation_check CHECK (
        (derived_from_script_unit_id IS NULL AND derivation_kind IS NULL)
        OR (derived_from_script_unit_id IS NOT NULL AND derivation_kind IN ('copy', 'language_variant', 'agent_idea'))
    ),
    CONSTRAINT commerce_script_units_revision_check CHECK (revision > 0),
    CONSTRAINT commerce_script_units_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT commerce_script_units_archive_check CHECK (
        (status <> 'archived' AND archived_at IS NULL)
        OR (status = 'archived' AND archived_at IS NOT NULL)
    ),
    CONSTRAINT commerce_script_units_ready_check CHECK (
        status <> 'ready'
        OR (current_source_version_id IS NOT NULL AND current_localization_id IS NOT NULL AND active_unit_generation_id IS NOT NULL)
    ),
    UNIQUE(project_id, unit_no),
    UNIQUE(id, product_id, organization_id, project_id)
);

CREATE UNIQUE INDEX commerce_script_units_active_sort_unique
    ON commerce_script_units(project_id, sort_order)
    WHERE status <> 'archived';

ALTER TABLE commerce_script_units
    ADD CONSTRAINT commerce_script_units_derived_from_fk
        FOREIGN KEY (derived_from_script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_units(id, product_id, organization_id, project_id) ON DELETE RESTRICT;

CREATE TABLE commerce_ad_script_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    version INTEGER NOT NULL,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    source_language_hint TEXT,
    detected_source_language TEXT,
    manual_override BOOLEAN NOT NULL DEFAULT false,
    source_version_id UUID,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_ad_script_versions_unit_fk
        FOREIGN KEY (script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_units(id, product_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_ad_script_versions_version_check CHECK (version > 0),
    CONSTRAINT commerce_ad_script_versions_content_check CHECK (trim(content) <> ''),
    CONSTRAINT commerce_ad_script_versions_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_ad_script_versions_source_hint_check CHECK (
        source_language_hint IS NULL OR source_language_hint ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
    ),
    CONSTRAINT commerce_ad_script_versions_detected_language_check CHECK (
        detected_source_language IS NULL OR detected_source_language ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
    ),
    UNIQUE(script_unit_id, version),
    UNIQUE(id, script_unit_id, product_id, organization_id, project_id)
);

ALTER TABLE commerce_ad_script_versions
    ADD CONSTRAINT commerce_ad_script_versions_source_fk
        FOREIGN KEY (source_version_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_versions(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT;

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_ad_script_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'commerce ad script versions are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_ad_script_versions_immutable
BEFORE UPDATE ON commerce_ad_script_versions
FOR EACH ROW EXECUTE FUNCTION protect_commerce_ad_script_version();

CREATE TABLE commerce_ad_script_segments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    script_version_id UUID NOT NULL,
    segment_no INTEGER NOT NULL,
    segment_kind TEXT NOT NULL,
    source_text TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    required BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_ad_script_segments_version_fk
        FOREIGN KEY (script_version_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_versions(id, script_unit_id, product_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_ad_script_segments_number_check CHECK (segment_no > 0),
    CONSTRAINT commerce_ad_script_segments_kind_check CHECK (trim(segment_kind) <> ''),
    CONSTRAINT commerce_ad_script_segments_text_check CHECK (trim(source_text) <> ''),
    CONSTRAINT commerce_ad_script_segments_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    UNIQUE(script_version_id, segment_no),
    UNIQUE(id, script_version_id, script_unit_id, product_id, organization_id, project_id)
);

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_ad_script_segment()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'commerce ad script segments are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_ad_script_segments_immutable
BEFORE UPDATE ON commerce_ad_script_segments
FOR EACH ROW EXECUTE FUNCTION protect_commerce_ad_script_segment();

CREATE TABLE commerce_language_resolutions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    source_script_version_id UUID NOT NULL,
    language_mode TEXT NOT NULL,
    source_language TEXT,
    target_language TEXT,
    confidence NUMERIC(5,4),
    reasoning TEXT NOT NULL DEFAULT '',
    needs_user_confirmation BOOLEAN NOT NULL DEFAULT false,
    status TEXT NOT NULL DEFAULT 'pending',
    confirmed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    confirmed_at TIMESTAMPTZ,
    prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE SET NULL,
    provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    input_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_language_resolutions_script_fk
        FOREIGN KEY (source_script_version_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_versions(id, script_unit_id, product_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_language_resolutions_mode_check CHECK (language_mode IN ('auto', 'explicit')),
    CONSTRAINT commerce_language_resolutions_source_check CHECK (
        source_language IS NULL OR source_language ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
    ),
    CONSTRAINT commerce_language_resolutions_target_check CHECK (
        target_language IS NULL OR target_language ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'
    ),
    CONSTRAINT commerce_language_resolutions_explicit_check CHECK (
        language_mode <> 'explicit' OR target_language IS NOT NULL
    ),
    CONSTRAINT commerce_language_resolutions_confidence_check CHECK (
        confidence IS NULL OR (confidence >= 0 AND confidence <= 1)
    ),
    CONSTRAINT commerce_language_resolutions_status_check
        CHECK (status IN ('pending', 'needs_confirmation', 'confirmed', 'rejected')),
    CONSTRAINT commerce_language_resolutions_confirmation_check CHECK (
        (status = 'confirmed' AND confirmed_by IS NOT NULL AND confirmed_at IS NOT NULL AND target_language IS NOT NULL)
        OR (status <> 'confirmed' AND confirmed_at IS NULL)
    ),
    CONSTRAINT commerce_language_resolutions_input_hash_check CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    UNIQUE(id, script_unit_id, source_script_version_id, product_id, organization_id, project_id)
);

CREATE TABLE commerce_ad_script_localizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    source_script_version_id UUID NOT NULL,
    language_resolution_id UUID NOT NULL,
    version INTEGER NOT NULL,
    source_language TEXT NOT NULL,
    target_language TEXT NOT NULL,
    localized_content TEXT NOT NULL,
    localized_content_hash TEXT NOT NULL,
    structured_contract JSONB NOT NULL,
    estimated_voiceover_seconds NUMERIC(10,3) NOT NULL,
    timing_analysis JSONB NOT NULL,
    timing_policy_version TEXT NOT NULL,
    review_status TEXT NOT NULL DEFAULT 'pending',
    reviewer_output JSONB NOT NULL DEFAULT '{}'::jsonb,
    prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE SET NULL,
    provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    revision BIGINT NOT NULL DEFAULT 1,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    approved_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    CONSTRAINT commerce_localizations_script_fk
        FOREIGN KEY (source_script_version_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_versions(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_localizations_resolution_fk
        FOREIGN KEY (language_resolution_id, script_unit_id, source_script_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_language_resolutions(id, script_unit_id, source_script_version_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_localizations_version_check CHECK (version > 0),
    CONSTRAINT commerce_localizations_source_language_check
        CHECK (source_language ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'),
    CONSTRAINT commerce_localizations_target_language_check
        CHECK (target_language ~ '^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$'),
    CONSTRAINT commerce_localizations_content_check CHECK (trim(localized_content) <> ''),
    CONSTRAINT commerce_localizations_hash_check CHECK (localized_content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_localizations_contract_check CHECK (jsonb_typeof(structured_contract) = 'object'),
    CONSTRAINT commerce_localizations_duration_check CHECK (estimated_voiceover_seconds >= 0),
    CONSTRAINT commerce_localizations_timing_check CHECK (jsonb_typeof(timing_analysis) = 'object'),
    CONSTRAINT commerce_localizations_policy_check CHECK (trim(timing_policy_version) <> ''),
    CONSTRAINT commerce_localizations_review_status_check
        CHECK (review_status IN ('pending', 'approved', 'rejected', 'changes_requested')),
    CONSTRAINT commerce_localizations_reviewer_check CHECK (jsonb_typeof(reviewer_output) = 'object'),
    CONSTRAINT commerce_localizations_status_check
        CHECK (status IN ('draft', 'reviewing', 'approved', 'rejected', 'archived')),
    CONSTRAINT commerce_localizations_revision_check CHECK (revision > 0),
    CONSTRAINT commerce_localizations_lifecycle_check CHECK (
        (status = 'approved' AND review_status = 'approved' AND approved_at IS NOT NULL AND archived_at IS NULL)
        OR (status = 'archived' AND archived_at IS NOT NULL)
        OR (status NOT IN ('approved', 'archived') AND approved_at IS NULL AND archived_at IS NULL)
    ),
    UNIQUE(script_unit_id, version),
    UNIQUE(id, script_unit_id, source_script_version_id, product_id, organization_id, project_id),
    UNIQUE(id, script_unit_id, product_id, organization_id, project_id)
);

CREATE TABLE commerce_localization_segments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    source_script_version_id UUID NOT NULL,
    localization_id UUID NOT NULL,
    source_segment_id UUID NOT NULL,
    segment_no INTEGER NOT NULL,
    sales_beat TEXT NOT NULL,
    localized_text TEXT NOT NULL,
    voiceover_text TEXT NOT NULL,
    onscreen_text TEXT NOT NULL DEFAULT '',
    product_claims JSONB NOT NULL DEFAULT '[]'::jsonb,
    required_product_features JSONB NOT NULL DEFAULT '[]'::jsonb,
    content_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_localization_segments_localization_fk
        FOREIGN KEY (localization_id, script_unit_id, source_script_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_localizations(id, script_unit_id, source_script_version_id, product_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_localization_segments_source_fk
        FOREIGN KEY (source_segment_id, source_script_version_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_segments(id, script_version_id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_localization_segments_number_check CHECK (segment_no > 0),
    CONSTRAINT commerce_localization_segments_sales_beat_check CHECK (trim(sales_beat) <> ''),
    CONSTRAINT commerce_localization_segments_localized_text_check CHECK (trim(localized_text) <> ''),
    CONSTRAINT commerce_localization_segments_claims_check CHECK (jsonb_typeof(product_claims) = 'array'),
    CONSTRAINT commerce_localization_segments_features_check CHECK (jsonb_typeof(required_product_features) = 'array'),
    CONSTRAINT commerce_localization_segments_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    UNIQUE(localization_id, segment_no),
    UNIQUE(localization_id, source_segment_id),
    UNIQUE(id, localization_id, script_unit_id, organization_id, project_id)
);

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_localization_snapshot()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.organization_id IS DISTINCT FROM OLD.organization_id
       OR NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.product_id IS DISTINCT FROM OLD.product_id
       OR NEW.script_unit_id IS DISTINCT FROM OLD.script_unit_id
       OR NEW.source_script_version_id IS DISTINCT FROM OLD.source_script_version_id
       OR NEW.language_resolution_id IS DISTINCT FROM OLD.language_resolution_id
       OR NEW.version IS DISTINCT FROM OLD.version
       OR NEW.source_language IS DISTINCT FROM OLD.source_language
       OR NEW.target_language IS DISTINCT FROM OLD.target_language
       OR NEW.localized_content IS DISTINCT FROM OLD.localized_content
       OR NEW.localized_content_hash IS DISTINCT FROM OLD.localized_content_hash
       OR NEW.structured_contract IS DISTINCT FROM OLD.structured_contract
       OR NEW.estimated_voiceover_seconds IS DISTINCT FROM OLD.estimated_voiceover_seconds
       OR NEW.timing_analysis IS DISTINCT FROM OLD.timing_analysis
       OR NEW.timing_policy_version IS DISTINCT FROM OLD.timing_policy_version
       OR NEW.prompt_version_id IS DISTINCT FROM OLD.prompt_version_id
       OR NEW.provider_call_id IS DISTINCT FROM OLD.provider_call_id
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'commerce localization content is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status IN ('approved', 'rejected', 'archived') AND NEW.status IS DISTINCT FROM OLD.status
       AND NOT (OLD.status = 'approved' AND NEW.status = 'archived') THEN
        RAISE EXCEPTION 'terminal commerce localization state is immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_localizations_snapshot_immutable
BEFORE UPDATE ON commerce_ad_script_localizations
FOR EACH ROW EXECUTE FUNCTION protect_commerce_localization_snapshot();

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_localization_segment()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'commerce localization segments are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_localization_segments_immutable
BEFORE UPDATE ON commerce_localization_segments
FOR EACH ROW EXECUTE FUNCTION protect_commerce_localization_segment();

CREATE TABLE commerce_script_unit_generations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product_id UUID NOT NULL,
    script_unit_id UUID NOT NULL,
    project_production_generation_id UUID NOT NULL,
    unit_generation_no BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'preparing',
    commerce_workflow_binding_id UUID NOT NULL,
    commerce_workflow_binding_revision BIGINT NOT NULL,
    product_version_id UUID NOT NULL,
    source_script_version_id UUID NOT NULL,
    localization_id UUID NOT NULL,
    reference_pack_id UUID NOT NULL,
    unit_configuration_snapshot JSONB NOT NULL,
    unit_configuration_hash TEXT NOT NULL,
    source_unit_generation_id UUID,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    CONSTRAINT commerce_unit_generations_unit_fk
        FOREIGN KEY (script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_units(id, product_id, organization_id, project_id) ON DELETE CASCADE,
    CONSTRAINT commerce_unit_generations_project_generation_fk
        FOREIGN KEY (project_production_generation_id, project_id, organization_id)
        REFERENCES project_video_production_generations(id, project_id, organization_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_unit_generations_binding_fk
        FOREIGN KEY (commerce_workflow_binding_id, project_id, organization_id, commerce_workflow_binding_revision)
        REFERENCES project_commerce_workflow_bindings(id, project_id, organization_id, binding_revision) ON DELETE RESTRICT,
    CONSTRAINT commerce_unit_generations_product_version_fk
        FOREIGN KEY (product_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_product_versions(id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_unit_generations_script_version_fk
        FOREIGN KEY (source_script_version_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_versions(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_unit_generations_localization_fk
        FOREIGN KEY (localization_id, script_unit_id, source_script_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_localizations(id, script_unit_id, source_script_version_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_unit_generations_reference_pack_fk
        FOREIGN KEY (reference_pack_id, product_id, product_version_id, organization_id, project_id)
        REFERENCES commerce_product_reference_packs(id, product_id, product_version_id, organization_id, project_id) ON DELETE RESTRICT,
    CONSTRAINT commerce_unit_generations_number_check CHECK (unit_generation_no > 0),
    CONSTRAINT commerce_unit_generations_status_check CHECK (status IN ('preparing', 'active', 'archived', 'failed')),
    CONSTRAINT commerce_unit_generations_binding_revision_check CHECK (commerce_workflow_binding_revision > 0),
    CONSTRAINT commerce_unit_generations_configuration_check CHECK (jsonb_typeof(unit_configuration_snapshot) = 'object'),
    CONSTRAINT commerce_unit_generations_hash_check CHECK (unit_configuration_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_unit_generations_lifecycle_check CHECK (
        (status = 'preparing' AND activated_at IS NULL AND archived_at IS NULL AND failed_at IS NULL)
        OR (status = 'active' AND activated_at IS NOT NULL AND archived_at IS NULL AND failed_at IS NULL)
        OR (status = 'archived' AND activated_at IS NOT NULL AND archived_at IS NOT NULL AND failed_at IS NULL)
        OR (status = 'failed' AND activated_at IS NULL AND archived_at IS NULL AND failed_at IS NOT NULL)
    ),
    UNIQUE(script_unit_id, unit_generation_no),
    UNIQUE(id, script_unit_id, product_id, organization_id, project_id),
    UNIQUE(id, script_unit_id, organization_id, project_id)
);

CREATE UNIQUE INDEX commerce_unit_generations_one_active
    ON commerce_script_unit_generations(script_unit_id)
    WHERE status = 'active';

ALTER TABLE commerce_script_unit_generations
    ADD CONSTRAINT commerce_unit_generations_source_fk
        FOREIGN KEY (source_unit_generation_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT;

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_unit_generation_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    selected_binding UUID;
    selected_revision BIGINT;
BEGIN
    SELECT generation.commerce_workflow_binding_id, binding.binding_revision
    INTO selected_binding, selected_revision
    FROM project_video_production_generations generation
    JOIN project_commerce_workflow_bindings binding
      ON binding.id = generation.commerce_workflow_binding_id
     AND binding.project_id = generation.project_id
     AND binding.organization_id = generation.organization_id
    WHERE generation.id = NEW.project_production_generation_id
      AND generation.project_id = NEW.project_id
      AND generation.organization_id = NEW.organization_id;

    IF selected_binding IS DISTINCT FROM NEW.commerce_workflow_binding_id
       OR selected_revision IS DISTINCT FROM NEW.commerce_workflow_binding_revision THEN
        RAISE EXCEPTION 'script unit generation binding identity mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_unit_generations_identity
BEFORE INSERT OR UPDATE OF organization_id, project_id, project_production_generation_id,
    commerce_workflow_binding_id, commerce_workflow_binding_revision
ON commerce_script_unit_generations
FOR EACH ROW EXECUTE FUNCTION validate_commerce_unit_generation_identity();

ALTER TABLE commerce_script_units
    ADD CONSTRAINT commerce_script_units_current_source_fk
        FOREIGN KEY (current_source_version_id, id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_versions(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY IMMEDIATE,
    ADD CONSTRAINT commerce_script_units_current_localization_fk
        FOREIGN KEY (current_localization_id, id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_localizations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY IMMEDIATE,
    ADD CONSTRAINT commerce_script_units_active_generation_fk
        FOREIGN KEY (active_unit_generation_id, id, product_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY IMMEDIATE;

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_script_unit_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.organization_id IS DISTINCT FROM OLD.organization_id
       OR NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.product_id IS DISTINCT FROM OLD.product_id
       OR NEW.unit_no IS DISTINCT FROM OLD.unit_no
       OR NEW.derived_from_script_unit_id IS DISTINCT FROM OLD.derived_from_script_unit_id
       OR NEW.derivation_kind IS DISTINCT FROM OLD.derivation_kind
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'commerce script unit identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'archived' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'archived commerce script units are immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.revision < OLD.revision OR NEW.unit_generation_no < OLD.unit_generation_no THEN
        RAISE EXCEPTION 'commerce script unit revisions are monotonic' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_script_units_identity_immutable
BEFORE UPDATE ON commerce_script_units
FOR EACH ROW EXECUTE FUNCTION protect_commerce_script_unit_identity();

ALTER TABLE commerce_setup_sessions
    ADD COLUMN product_id UUID,
    ADD COLUMN script_unit_id UUID,
    ADD COLUMN source_script_version_id UUID,
    ADD COLUMN localization_id UUID,
    ADD CONSTRAINT commerce_setup_sessions_product_fk
        FOREIGN KEY (product_id, organization_id, project_id)
        REFERENCES commerce_products(id, organization_id, project_id) ON DELETE CASCADE,
    ADD CONSTRAINT commerce_setup_sessions_script_unit_fk
        FOREIGN KEY (script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_script_units(id, product_id, organization_id, project_id) ON DELETE CASCADE,
    ADD CONSTRAINT commerce_setup_sessions_script_version_fk
        FOREIGN KEY (source_script_version_id, script_unit_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_versions(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT commerce_setup_sessions_localization_fk
        FOREIGN KEY (localization_id, script_unit_id, source_script_version_id, product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_localizations(id, script_unit_id, source_script_version_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT commerce_setup_sessions_subject_check CHECK (
        scope_type = 'project'
        OR (scope_type = 'script_unit' AND project_id IS NOT NULL AND product_id IS NOT NULL AND script_unit_id IS NOT NULL)
    );

CREATE INDEX commerce_setup_sessions_script_unit_state_idx
    ON commerce_setup_sessions(script_unit_id, state, updated_at DESC)
    WHERE script_unit_id IS NOT NULL;

-- +goose Down

SET search_path TO public;

DROP INDEX IF EXISTS commerce_setup_sessions_script_unit_state_idx;

ALTER TABLE commerce_setup_sessions
    DROP CONSTRAINT IF EXISTS commerce_setup_sessions_subject_check,
    DROP CONSTRAINT IF EXISTS commerce_setup_sessions_localization_fk,
    DROP CONSTRAINT IF EXISTS commerce_setup_sessions_script_version_fk,
    DROP CONSTRAINT IF EXISTS commerce_setup_sessions_script_unit_fk,
    DROP CONSTRAINT IF EXISTS commerce_setup_sessions_product_fk,
    DROP COLUMN IF EXISTS localization_id,
    DROP COLUMN IF EXISTS source_script_version_id,
    DROP COLUMN IF EXISTS script_unit_id,
    DROP COLUMN IF EXISTS product_id;

DROP TRIGGER IF EXISTS commerce_script_units_identity_immutable ON commerce_script_units;
DROP FUNCTION IF EXISTS protect_commerce_script_unit_identity();

ALTER TABLE commerce_script_units
    DROP CONSTRAINT IF EXISTS commerce_script_units_active_generation_fk,
    DROP CONSTRAINT IF EXISTS commerce_script_units_current_localization_fk,
    DROP CONSTRAINT IF EXISTS commerce_script_units_current_source_fk;

DROP TRIGGER IF EXISTS commerce_unit_generations_identity ON commerce_script_unit_generations;
DROP FUNCTION IF EXISTS validate_commerce_unit_generation_identity();
DROP TABLE IF EXISTS commerce_script_unit_generations;

DROP TRIGGER IF EXISTS commerce_localization_segments_immutable ON commerce_localization_segments;
DROP FUNCTION IF EXISTS protect_commerce_localization_segment();
DROP TABLE IF EXISTS commerce_localization_segments;

DROP TRIGGER IF EXISTS commerce_localizations_snapshot_immutable ON commerce_ad_script_localizations;
DROP FUNCTION IF EXISTS protect_commerce_localization_snapshot();
DROP TABLE IF EXISTS commerce_ad_script_localizations;
DROP TABLE IF EXISTS commerce_language_resolutions;

DROP TRIGGER IF EXISTS commerce_ad_script_segments_immutable ON commerce_ad_script_segments;
DROP FUNCTION IF EXISTS protect_commerce_ad_script_segment();
DROP TABLE IF EXISTS commerce_ad_script_segments;

DROP TRIGGER IF EXISTS commerce_ad_script_versions_immutable ON commerce_ad_script_versions;
DROP FUNCTION IF EXISTS protect_commerce_ad_script_version();
DROP TABLE IF EXISTS commerce_ad_script_versions;
DROP TABLE IF EXISTS commerce_script_units;

DROP TRIGGER IF EXISTS commerce_reference_pack_items_immutable ON commerce_product_reference_pack_items;
DROP FUNCTION IF EXISTS protect_commerce_reference_pack_item();
DROP TRIGGER IF EXISTS commerce_reference_packs_immutable ON commerce_product_reference_packs;
DROP FUNCTION IF EXISTS protect_commerce_reference_pack();
DROP TRIGGER IF EXISTS commerce_reference_pack_items_snapshot ON commerce_product_reference_pack_items;
DROP TRIGGER IF EXISTS commerce_reference_packs_snapshot ON commerce_product_reference_packs;
DROP FUNCTION IF EXISTS validate_commerce_reference_pack_snapshot();
DROP TABLE IF EXISTS commerce_product_reference_pack_items;
DROP TABLE IF EXISTS commerce_product_reference_packs;

DROP INDEX IF EXISTS commerce_product_reference_uploads_cleanup_idx;
DROP TABLE IF EXISTS commerce_product_reference_uploads;

DROP TRIGGER IF EXISTS commerce_product_references_media_identity ON commerce_product_references;
DROP FUNCTION IF EXISTS validate_commerce_product_reference_media();
DROP TABLE IF EXISTS commerce_product_references;

DROP TRIGGER IF EXISTS commerce_product_versions_immutable ON commerce_product_versions;
DROP FUNCTION IF EXISTS protect_commerce_product_version();

ALTER TABLE commerce_products
    DROP CONSTRAINT IF EXISTS commerce_products_current_version_fk;

DROP TABLE IF EXISTS commerce_product_versions;
DROP TRIGGER IF EXISTS commerce_products_project_kind ON commerce_products;
DROP FUNCTION IF EXISTS validate_commerce_product_project_kind();
DROP TABLE IF EXISTS commerce_products;

ALTER TABLE project_commerce_workflow_bindings
    DROP CONSTRAINT IF EXISTS commerce_bindings_identity_revision_unique;

ALTER TABLE media_files
    DROP CONSTRAINT IF EXISTS media_files_id_organization_project_unique;

ALTER TABLE artifacts
    DROP CONSTRAINT IF EXISTS artifacts_id_organization_project_unique;
