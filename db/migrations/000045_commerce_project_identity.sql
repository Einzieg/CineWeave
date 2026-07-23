-- +goose Up

SET search_path TO public;

ALTER TABLE projects
    ADD COLUMN project_kind TEXT;

UPDATE projects
SET project_kind = 'narrative',
    project_type = CASE trim(COALESCE(project_type, ''))
        WHEN '短片' THEN 'short_film'
        WHEN '漫剧' THEN 'comic_drama'
        WHEN '广告' THEN 'brand_ad'
        WHEN '品牌广告' THEN 'brand_ad'
        WHEN '角色 IP' THEN 'character_ip'
        WHEN '角色IP' THEN 'character_ip'
        WHEN '其他' THEN 'other'
        WHEN 'silent_video' THEN 'short_film'
        WHEN 'short_video' THEN 'short_film'
        WHEN 'film' THEN 'short_film'
        WHEN '' THEN 'short_film'
        ELSE project_type
    END,
    content_type = CASE trim(COALESCE(content_type, ''))
        WHEN '小说改编' THEN 'novel'
        WHEN '剧本创作' THEN 'script'
        WHEN '分镜先行' THEN 'storyboard_first'
        WHEN '自定义' THEN 'original'
        WHEN '' THEN 'script'
        ELSE content_type
    END;

ALTER TABLE projects
    ALTER COLUMN project_kind SET DEFAULT 'narrative',
    ALTER COLUMN project_kind SET NOT NULL,
    ADD CONSTRAINT projects_kind_check
        CHECK (project_kind IN ('narrative', 'commerce_video')),
    ADD CONSTRAINT projects_kind_configuration_check
        CHECK (
            (
                project_kind = 'narrative'
                AND project_type IN ('short_film', 'comic_drama', 'brand_ad', 'character_ip', 'other')
                AND content_type IN ('novel', 'script', 'storyboard_first', 'original')
            )
            OR (
                project_kind = 'commerce_video'
                AND project_type = 'commerce_video'
                AND content_type IS NULL
            )
        ),
    ADD CONSTRAINT projects_id_organization_unique UNIQUE(id, organization_id);

ALTER TABLE projects
    ALTER COLUMN active_video_production_generation_id DROP NOT NULL,
    ALTER COLUMN video_production_generation_no DROP NOT NULL,
    DROP CONSTRAINT projects_video_production_state_check,
    ADD CONSTRAINT projects_video_production_state_check CHECK (
        video_production_state IN ('unconfigured', 'storyboard_required', 'ready', 'rebuilding', 'blocked', 'reconfiguration_required')
    );

-- +goose StatementBegin
CREATE FUNCTION protect_project_kind()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.project_kind IS DISTINCT FROM OLD.project_kind THEN
        RAISE EXCEPTION 'project kind is immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER projects_kind_immutable
BEFORE UPDATE OF project_kind ON projects
FOR EACH ROW EXECUTE FUNCTION protect_project_kind();

ALTER TABLE project_video_production_bindings
    DROP CONSTRAINT project_video_production_bindings_status_check,
    DROP CONSTRAINT project_video_production_bindings_superseded_check,
    ADD CONSTRAINT project_video_production_bindings_status_check
        CHECK (status IN ('preparing', 'active', 'superseded', 'failed')),
    ADD CONSTRAINT project_video_production_bindings_superseded_check CHECK (
        (status IN ('preparing', 'active', 'failed') AND superseded_at IS NULL)
        OR (status = 'superseded' AND superseded_at IS NOT NULL)
    );

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION protect_project_video_production_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.profile_version_id IS DISTINCT FROM OLD.profile_version_id
       OR NEW.compatibility_policy IS DISTINCT FROM OLD.compatibility_policy
       OR NEW.overrides IS DISTINCT FROM OLD.overrides
       OR NEW.profile_snapshot IS DISTINCT FROM OLD.profile_snapshot
       OR NEW.profile_snapshot_hash IS DISTINCT FROM OLD.profile_snapshot_hash
       OR NEW.revision IS DISTINCT FROM OLD.revision
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'video production bindings are append-only' USING ERRCODE = '55000';
    END IF;
    IF OLD.status IN ('superseded', 'failed') AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'terminal video production bindings are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'preparing' AND NEW.status NOT IN ('preparing', 'active', 'failed') THEN
        RAISE EXCEPTION 'invalid preparing video production binding transition' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'active' AND NEW.status NOT IN ('active', 'superseded') THEN
        RAISE EXCEPTION 'invalid active video production binding transition' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TABLE commerce_workflow_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    template_key TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_workflow_templates_key_check CHECK (trim(template_key) <> ''),
    CONSTRAINT commerce_workflow_templates_name_check CHECK (trim(name) <> ''),
    CONSTRAINT commerce_workflow_templates_status_check CHECK (status IN ('active', 'retired')),
    UNIQUE(id, organization_id)
);

CREATE UNIQUE INDEX commerce_workflow_templates_system_key_unique
    ON commerce_workflow_templates(template_key)
    WHERE organization_id IS NULL;

CREATE UNIQUE INDEX commerce_workflow_templates_organization_key_unique
    ON commerce_workflow_templates(organization_id, template_key)
    WHERE organization_id IS NOT NULL;

CREATE TABLE commerce_workflow_template_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id UUID NOT NULL REFERENCES commerce_workflow_templates(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    configuration_snapshot JSONB NOT NULL,
    prompt_bindings JSONB NOT NULL,
    agent_model_contracts JSONB NOT NULL,
    language_contract JSONB NOT NULL,
    image_capability_contract JSONB NOT NULL,
    video_capability_contract JSONB NOT NULL,
    video_production_profile_version_id UUID NOT NULL
        REFERENCES video_production_profile_versions(id) ON DELETE RESTRICT,
    content_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    retired_at TIMESTAMPTZ,
    CONSTRAINT commerce_workflow_template_versions_version_check CHECK (version > 0),
    CONSTRAINT commerce_workflow_template_versions_configuration_check CHECK (jsonb_typeof(configuration_snapshot) = 'object'),
    CONSTRAINT commerce_workflow_template_versions_prompt_check CHECK (jsonb_typeof(prompt_bindings) = 'object'),
    CONSTRAINT commerce_workflow_template_versions_agent_models_check CHECK (jsonb_typeof(agent_model_contracts) = 'object'),
    CONSTRAINT commerce_workflow_template_versions_language_check CHECK (jsonb_typeof(language_contract) = 'object'),
    CONSTRAINT commerce_workflow_template_versions_image_check CHECK (jsonb_typeof(image_capability_contract) = 'object'),
    CONSTRAINT commerce_workflow_template_versions_video_check CHECK (jsonb_typeof(video_capability_contract) = 'object'),
    CONSTRAINT commerce_workflow_template_versions_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_workflow_template_versions_status_check CHECK (status IN ('draft', 'published', 'retired')),
    CONSTRAINT commerce_workflow_template_versions_lifecycle_check CHECK (
        (status = 'draft' AND published_at IS NULL AND retired_at IS NULL)
        OR (status = 'published' AND published_at IS NOT NULL AND retired_at IS NULL)
        OR (status = 'retired' AND published_at IS NOT NULL AND retired_at IS NOT NULL)
    ),
    UNIQUE(template_id, version),
    UNIQUE(id, template_id)
);

CREATE INDEX commerce_workflow_template_versions_lookup_idx
    ON commerce_workflow_template_versions(template_id, status, version DESC);

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_workflow_template_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status IN ('published', 'retired') AND (
        NEW.template_id IS DISTINCT FROM OLD.template_id
        OR NEW.version IS DISTINCT FROM OLD.version
        OR NEW.configuration_snapshot IS DISTINCT FROM OLD.configuration_snapshot
        OR NEW.prompt_bindings IS DISTINCT FROM OLD.prompt_bindings
        OR NEW.agent_model_contracts IS DISTINCT FROM OLD.agent_model_contracts
        OR NEW.language_contract IS DISTINCT FROM OLD.language_contract
        OR NEW.image_capability_contract IS DISTINCT FROM OLD.image_capability_contract
        OR NEW.video_capability_contract IS DISTINCT FROM OLD.video_capability_contract
        OR NEW.video_production_profile_version_id IS DISTINCT FROM OLD.video_production_profile_version_id
        OR NEW.content_hash IS DISTINCT FROM OLD.content_hash
        OR NEW.created_by IS DISTINCT FROM OLD.created_by
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
    ) THEN
        RAISE EXCEPTION 'published commerce workflow template versions are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'retired' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'retired commerce workflow template versions are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'draft' AND NEW.status NOT IN ('draft', 'published') THEN
        RAISE EXCEPTION 'invalid commerce workflow template version transition' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'published' AND NEW.status NOT IN ('published', 'retired') THEN
        RAISE EXCEPTION 'invalid commerce workflow template version transition' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_workflow_template_versions_immutable
BEFORE UPDATE ON commerce_workflow_template_versions
FOR EACH ROW EXECUTE FUNCTION protect_commerce_workflow_template_version();

CREATE TABLE project_commerce_workflow_bindings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    project_id UUID NOT NULL,
    template_version_id UUID NOT NULL REFERENCES commerce_workflow_template_versions(id) ON DELETE RESTRICT,
    video_production_binding_id UUID NOT NULL,
    binding_revision BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'preparing',
    configuration_snapshot JSONB NOT NULL,
    configuration_hash TEXT NOT NULL,
    video_profile_snapshot_hash TEXT NOT NULL,
    model_routing_snapshot JSONB NOT NULL,
    capability_snapshot JSONB NOT NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ,
    superseded_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    CONSTRAINT project_commerce_workflow_bindings_project_fk
        FOREIGN KEY (project_id, organization_id)
        REFERENCES projects(id, organization_id) ON DELETE CASCADE,
    CONSTRAINT project_commerce_workflow_bindings_video_binding_fk
        FOREIGN KEY (video_production_binding_id, project_id)
        REFERENCES project_video_production_bindings(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT project_commerce_workflow_bindings_revision_check CHECK (binding_revision > 0),
    CONSTRAINT project_commerce_workflow_bindings_status_check CHECK (status IN ('preparing', 'active', 'superseded', 'failed')),
    CONSTRAINT project_commerce_workflow_bindings_configuration_check CHECK (jsonb_typeof(configuration_snapshot) = 'object'),
    CONSTRAINT project_commerce_workflow_bindings_routing_check CHECK (jsonb_typeof(model_routing_snapshot) = 'object'),
    CONSTRAINT project_commerce_workflow_bindings_capability_check CHECK (jsonb_typeof(capability_snapshot) = 'object'),
    CONSTRAINT project_commerce_workflow_bindings_configuration_hash_check CHECK (configuration_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT project_commerce_workflow_bindings_profile_hash_check CHECK (video_profile_snapshot_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT project_commerce_workflow_bindings_lifecycle_check CHECK (
        (status = 'preparing' AND activated_at IS NULL AND superseded_at IS NULL AND failed_at IS NULL)
        OR (status = 'active' AND activated_at IS NOT NULL AND superseded_at IS NULL AND failed_at IS NULL)
        OR (status = 'superseded' AND activated_at IS NOT NULL AND superseded_at IS NOT NULL AND failed_at IS NULL)
        OR (status = 'failed' AND activated_at IS NULL AND superseded_at IS NULL AND failed_at IS NOT NULL)
    ),
    UNIQUE(project_id, binding_revision),
    UNIQUE(id, project_id),
    UNIQUE(id, project_id, organization_id)
);

CREATE UNIQUE INDEX project_commerce_workflow_bindings_one_active
    ON project_commerce_workflow_bindings(project_id)
    WHERE status = 'active';

CREATE INDEX project_commerce_workflow_bindings_template_idx
    ON project_commerce_workflow_bindings(template_version_id, status);

-- +goose StatementBegin
CREATE FUNCTION protect_project_commerce_workflow_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.organization_id IS DISTINCT FROM OLD.organization_id
       OR NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.template_version_id IS DISTINCT FROM OLD.template_version_id
       OR NEW.video_production_binding_id IS DISTINCT FROM OLD.video_production_binding_id
       OR NEW.binding_revision IS DISTINCT FROM OLD.binding_revision
       OR NEW.configuration_snapshot IS DISTINCT FROM OLD.configuration_snapshot
       OR NEW.configuration_hash IS DISTINCT FROM OLD.configuration_hash
       OR NEW.video_profile_snapshot_hash IS DISTINCT FROM OLD.video_profile_snapshot_hash
       OR NEW.model_routing_snapshot IS DISTINCT FROM OLD.model_routing_snapshot
       OR NEW.capability_snapshot IS DISTINCT FROM OLD.capability_snapshot
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'commerce workflow bindings are append-only' USING ERRCODE = '55000';
    END IF;
    IF OLD.status IN ('superseded', 'failed') AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'terminal commerce workflow bindings are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'preparing' AND NEW.status NOT IN ('preparing', 'active', 'failed') THEN
        RAISE EXCEPTION 'invalid preparing commerce workflow binding transition' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'active' AND NEW.status NOT IN ('active', 'superseded') THEN
        RAISE EXCEPTION 'invalid active commerce workflow binding transition' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_commerce_workflow_bindings_append_only
BEFORE UPDATE ON project_commerce_workflow_bindings
FOR EACH ROW EXECUTE FUNCTION protect_project_commerce_workflow_binding();

ALTER TABLE project_video_production_generations
    ADD COLUMN commerce_workflow_binding_id UUID,
    ADD CONSTRAINT project_video_production_generations_id_project_organization_unique
        UNIQUE(id, project_id, organization_id),
    ADD CONSTRAINT project_video_production_generations_commerce_binding_fk
        FOREIGN KEY (commerce_workflow_binding_id, project_id, organization_id)
        REFERENCES project_commerce_workflow_bindings(id, project_id, organization_id) ON DELETE RESTRICT;

-- +goose StatementBegin
CREATE FUNCTION validate_project_video_production_generation_commerce_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    selected_kind TEXT;
    commerce_video_binding UUID;
BEGIN
    SELECT project_kind
    INTO selected_kind
    FROM projects
    WHERE id = NEW.project_id
      AND organization_id = NEW.organization_id;

    IF selected_kind IS NULL THEN
        RAISE EXCEPTION 'production generation project identity is invalid' USING ERRCODE = '23503';
    END IF;

    IF selected_kind = 'narrative' THEN
        IF NEW.commerce_workflow_binding_id IS NOT NULL THEN
            RAISE EXCEPTION 'narrative production generation cannot reference a commerce workflow binding' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.commerce_workflow_binding_id IS NULL THEN
        RAISE EXCEPTION 'commerce production generation requires a commerce workflow binding' USING ERRCODE = '23514';
    END IF;

    SELECT video_production_binding_id
    INTO commerce_video_binding
    FROM project_commerce_workflow_bindings
    WHERE id = NEW.commerce_workflow_binding_id
      AND project_id = NEW.project_id
      AND organization_id = NEW.organization_id;

    IF commerce_video_binding IS NULL OR commerce_video_binding <> NEW.binding_id THEN
        RAISE EXCEPTION 'commerce and video production bindings do not match' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_video_production_generations_commerce_identity
BEFORE INSERT OR UPDATE OF organization_id, project_id, binding_id, commerce_workflow_binding_id
ON project_video_production_generations
FOR EACH ROW EXECUTE FUNCTION validate_project_video_production_generation_commerce_identity();

CREATE TABLE commerce_setup_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id UUID,
    workflow_template_version_id UUID NOT NULL
        REFERENCES commerce_workflow_template_versions(id) ON DELETE RESTRICT,
    idempotency_scope TEXT NOT NULL,
    client_request_id TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'draft',
    step TEXT NOT NULL DEFAULT 'draft',
    revision BIGINT NOT NULL DEFAULT 1,
    input_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    input_hash TEXT NOT NULL,
    setup_attempt INTEGER NOT NULL DEFAULT 0,
    setup_workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    production_workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    last_error_code TEXT,
    last_error_message TEXT,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    CONSTRAINT commerce_setup_sessions_project_fk
        FOREIGN KEY (project_id, organization_id)
        REFERENCES projects(id, organization_id) ON DELETE CASCADE,
    CONSTRAINT commerce_setup_sessions_scope_check CHECK (scope_type IN ('project', 'script_unit')),
    CONSTRAINT commerce_setup_sessions_state_check CHECK (state IN (
        'draft', 'uploading', 'resolving_language', 'waiting_user_confirmation',
        'localizing', 'validating', 'needs_user_review', 'ready', 'starting',
        'started', 'completed', 'failed', 'abandoned'
    )),
    CONSTRAINT commerce_setup_sessions_idempotency_scope_check CHECK (trim(idempotency_scope) <> ''),
    CONSTRAINT commerce_setup_sessions_client_request_check CHECK (trim(client_request_id) <> ''),
    CONSTRAINT commerce_setup_sessions_request_hash_check CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_setup_sessions_input_check CHECK (jsonb_typeof(input_snapshot) = 'object'),
    CONSTRAINT commerce_setup_sessions_input_hash_check CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_setup_sessions_revision_check CHECK (revision > 0),
    CONSTRAINT commerce_setup_sessions_attempt_check CHECK (setup_attempt >= 0),
    CONSTRAINT commerce_setup_sessions_completion_check CHECK (
        (state = 'completed' AND completed_at IS NOT NULL)
        OR (state <> 'completed' AND completed_at IS NULL)
    ),
    UNIQUE(organization_id, idempotency_scope, client_request_id),
    UNIQUE(id, organization_id)
);

CREATE INDEX commerce_setup_sessions_project_state_idx
    ON commerce_setup_sessions(project_id, state, updated_at DESC)
    WHERE project_id IS NOT NULL;

CREATE INDEX commerce_setup_sessions_expiry_idx
    ON commerce_setup_sessions(expires_at)
    WHERE state IN ('draft', 'uploading', 'failed', 'abandoned');

-- +goose Down

SET search_path TO public;

DROP INDEX IF EXISTS commerce_setup_sessions_expiry_idx;
DROP INDEX IF EXISTS commerce_setup_sessions_project_state_idx;
DROP TABLE IF EXISTS commerce_setup_sessions;

DROP TRIGGER IF EXISTS project_video_production_generations_commerce_identity ON project_video_production_generations;
DROP FUNCTION IF EXISTS validate_project_video_production_generation_commerce_identity();

ALTER TABLE project_video_production_generations
    DROP CONSTRAINT IF EXISTS project_video_production_generations_commerce_binding_fk,
    DROP CONSTRAINT IF EXISTS project_video_production_generations_id_project_organization_unique,
    DROP COLUMN IF EXISTS commerce_workflow_binding_id;

DROP TRIGGER IF EXISTS project_commerce_workflow_bindings_append_only ON project_commerce_workflow_bindings;
DROP FUNCTION IF EXISTS protect_project_commerce_workflow_binding();
DROP TABLE IF EXISTS project_commerce_workflow_bindings;

DROP TRIGGER IF EXISTS commerce_workflow_template_versions_immutable ON commerce_workflow_template_versions;
DROP FUNCTION IF EXISTS protect_commerce_workflow_template_version();
DROP TABLE IF EXISTS commerce_workflow_template_versions;
DROP TABLE IF EXISTS commerce_workflow_templates;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM project_video_production_bindings
        WHERE status IN ('preparing', 'failed')
    ) THEN
        RAISE EXCEPTION 'cannot restore legacy video binding lifecycle while preparing or failed bindings exist';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE project_video_production_bindings
    DROP CONSTRAINT project_video_production_bindings_status_check,
    DROP CONSTRAINT project_video_production_bindings_superseded_check,
    ADD CONSTRAINT project_video_production_bindings_status_check
        CHECK (status IN ('active', 'superseded')),
    ADD CONSTRAINT project_video_production_bindings_superseded_check CHECK (
        (status = 'active' AND superseded_at IS NULL)
        OR (status = 'superseded' AND superseded_at IS NOT NULL)
    );

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION protect_project_video_production_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.profile_version_id IS DISTINCT FROM OLD.profile_version_id
       OR NEW.compatibility_policy IS DISTINCT FROM OLD.compatibility_policy
       OR NEW.overrides IS DISTINCT FROM OLD.overrides
       OR NEW.profile_snapshot IS DISTINCT FROM OLD.profile_snapshot
       OR NEW.profile_snapshot_hash IS DISTINCT FROM OLD.profile_snapshot_hash
       OR NEW.revision IS DISTINCT FROM OLD.revision
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'video production bindings are append-only' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'superseded' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'superseded video production bindings are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'active' AND NEW.status NOT IN ('active', 'superseded') THEN
        RAISE EXCEPTION 'invalid video production binding transition' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS projects_kind_immutable ON projects;
DROP FUNCTION IF EXISTS protect_project_kind();

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM projects
        WHERE active_video_production_generation_id IS NULL
           OR video_production_generation_no IS NULL
           OR video_production_state IN ('unconfigured', 'reconfiguration_required')
    ) THEN
        RAISE EXCEPTION 'cannot restore required project production generations while unconfigured projects exist';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE projects
    DROP CONSTRAINT projects_video_production_state_check,
    ADD CONSTRAINT projects_video_production_state_check CHECK (
        video_production_state IN ('storyboard_required', 'ready', 'rebuilding', 'blocked')
    ),
    ALTER COLUMN active_video_production_generation_id SET NOT NULL,
    ALTER COLUMN video_production_generation_no SET NOT NULL;

ALTER TABLE projects
    DROP CONSTRAINT IF EXISTS projects_id_organization_unique,
    DROP CONSTRAINT IF EXISTS projects_kind_configuration_check,
    DROP CONSTRAINT IF EXISTS projects_kind_check,
    DROP COLUMN IF EXISTS project_kind;
