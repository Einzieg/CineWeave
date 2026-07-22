-- +goose Up

SET search_path TO public;

ALTER TABLE project_sources
    ADD COLUMN content_revision BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN content_hash TEXT;

UPDATE project_sources
SET content_hash = encode(digest(convert_to(jsonb_build_object(
    'sourceType', source_type,
    'title', title,
    'content', content,
    'contentFormat', content_format
)::text, 'UTF8'), 'sha256'), 'hex');

ALTER TABLE project_sources
    ALTER COLUMN content_hash SET NOT NULL,
    ADD CONSTRAINT project_sources_content_revision_check CHECK (content_revision > 0),
    ADD CONSTRAINT project_sources_content_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$');

-- +goose StatementBegin
CREATE FUNCTION refresh_project_source_content_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.content_revision := GREATEST(COALESCE(NEW.content_revision, 1), 1);
    ELSIF ROW(NEW.source_type, NEW.title, NEW.content, NEW.content_format)
        IS DISTINCT FROM ROW(OLD.source_type, OLD.title, OLD.content, OLD.content_format) THEN
        NEW.content_revision := OLD.content_revision + 1;
    ELSE
        NEW.content_revision := OLD.content_revision;
    END IF;

    NEW.content_hash := encode(digest(convert_to(jsonb_build_object(
        'sourceType', NEW.source_type,
        'title', NEW.title,
        'content', NEW.content,
        'contentFormat', NEW.content_format
    )::text, 'UTF8'), 'sha256'), 'hex');
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_sources_refresh_content_identity
BEFORE INSERT OR UPDATE ON project_sources
FOR EACH ROW EXECUTE FUNCTION refresh_project_source_content_identity();

ALTER TABLE novel_chapters
    ADD COLUMN content_revision BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN content_hash TEXT;

UPDATE novel_chapters
SET content_hash = encode(digest(convert_to(jsonb_build_object(
    'chapterIndex', chapter_index,
    'volumeIndex', volume_index,
    'sectionIndex', section_index,
    'volumeTitle', volume_title,
    'chapterTitle', chapter_title,
    'content', content
)::text, 'UTF8'), 'sha256'), 'hex');

ALTER TABLE novel_chapters
    ALTER COLUMN content_hash SET NOT NULL,
    ADD CONSTRAINT novel_chapters_content_revision_check CHECK (content_revision > 0),
    ADD CONSTRAINT novel_chapters_content_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$');

-- +goose StatementBegin
CREATE FUNCTION refresh_novel_chapter_content_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.content_revision := GREATEST(COALESCE(NEW.content_revision, 1), 1);
    ELSIF ROW(
        NEW.chapter_index,
        NEW.volume_index,
        NEW.section_index,
        NEW.volume_title,
        NEW.chapter_title,
        NEW.content
    ) IS DISTINCT FROM ROW(
        OLD.chapter_index,
        OLD.volume_index,
        OLD.section_index,
        OLD.volume_title,
        OLD.chapter_title,
        OLD.content
    ) THEN
        NEW.content_revision := OLD.content_revision + 1;
    ELSE
        NEW.content_revision := OLD.content_revision;
    END IF;

    NEW.content_hash := encode(digest(convert_to(jsonb_build_object(
        'chapterIndex', NEW.chapter_index,
        'volumeIndex', NEW.volume_index,
        'sectionIndex', NEW.section_index,
        'volumeTitle', NEW.volume_title,
        'chapterTitle', NEW.chapter_title,
        'content', NEW.content
    )::text, 'UTF8'), 'sha256'), 'hex');
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER novel_chapters_refresh_content_identity
BEFORE INSERT OR UPDATE ON novel_chapters
FOR EACH ROW EXECUTE FUNCTION refresh_novel_chapter_content_identity();

ALTER TABLE script_versions DROP CONSTRAINT script_versions_status_check;
ALTER TABLE script_versions
    ADD CONSTRAINT script_versions_status_check CHECK (status = ANY (ARRAY['active', 'draft', 'partial', 'archived']));

ALTER TABLE scripts
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT scripts_revision_check CHECK (revision > 0);

-- +goose StatementBegin
CREATE FUNCTION maintain_script_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND ROW(NEW.title, NEW.current_version_id, NEW.source_id, NEW.status)
        IS DISTINCT FROM ROW(OLD.title, OLD.current_version_id, OLD.source_id, OLD.status) THEN
        NEW.revision := OLD.revision + 1;
    ELSE
        NEW.revision := COALESCE(OLD.revision, NEW.revision, 1);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER scripts_maintain_revision
BEFORE UPDATE ON scripts
FOR EACH ROW EXECUTE FUNCTION maintain_script_revision();

CREATE TABLE source_to_script_generations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow_run_id UUID NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    root_generation_id UUID REFERENCES source_to_script_generations(id) ON DELETE SET NULL,
    retry_of_generation_id UUID REFERENCES source_to_script_generations(id) ON DELETE SET NULL,
    attempt_generation INTEGER NOT NULL,
    source_id UUID NOT NULL REFERENCES project_sources(id) ON DELETE CASCADE,
    source_type TEXT NOT NULL,
    source_revision BIGINT NOT NULL,
    source_content_hash TEXT NOT NULL,
    source_snapshot_hash TEXT NOT NULL,
    script_id UUID NOT NULL REFERENCES scripts(id) ON DELETE CASCADE,
    expected_active_script_id UUID,
    expected_current_version_id UUID,
    expected_script_revision BIGINT NOT NULL,
    base_script_version_id UUID,
    result_script_version_id UUID REFERENCES script_versions(id) ON DELETE SET NULL,
    prompt_template_key TEXT NOT NULL,
    prompt_version_id UUID,
    prompt_content_hash TEXT NOT NULL,
    model_profile_key TEXT NOT NULL,
    provider_model_id UUID,
    project_snapshot JSONB NOT NULL,
    manual_bindings JSONB NOT NULL DEFAULT '[]'::jsonb,
    model_bindings JSONB NOT NULL DEFAULT '[]'::jsonb,
    manifest JSONB NOT NULL,
    manifest_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'prepared',
    idempotency_key TEXT,
    error_code TEXT,
    error_message TEXT,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finalized_at TIMESTAMPTZ,
    retention_expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + INTERVAL '30 days'),
    payload_purged_at TIMESTAMPTZ,
    CONSTRAINT source_to_script_generations_attempt_check CHECK (attempt_generation > 0),
    CONSTRAINT source_to_script_generations_source_revision_check CHECK (source_revision > 0),
    CONSTRAINT source_to_script_generations_script_revision_check CHECK (expected_script_revision > 0),
    CONSTRAINT source_to_script_generations_source_hash_check CHECK (source_content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT source_to_script_generations_snapshot_hash_check CHECK (source_snapshot_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT source_to_script_generations_prompt_hash_check CHECK (prompt_content_hash ~ '^(sha256:)?[0-9a-f]{64}$'),
    CONSTRAINT source_to_script_generations_manifest_hash_check CHECK (manifest_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT source_to_script_generations_status_check CHECK (status = ANY (ARRAY[
        'prepared', 'running', 'finalizing', 'succeeded', 'partial_succeeded', 'failed', 'replan_required'
    ])),
    CONSTRAINT source_to_script_generations_workflow_attempt_unique UNIQUE (workflow_run_id, attempt_generation)
);

CREATE INDEX source_to_script_generations_root_idx
    ON source_to_script_generations(root_generation_id, attempt_generation, created_at);

CREATE INDEX source_to_script_generations_project_status_idx
    ON source_to_script_generations(project_id, status, created_at DESC);

CREATE INDEX source_to_script_generations_retention_idx
    ON source_to_script_generations(retention_expires_at, id)
    WHERE payload_purged_at IS NULL
      AND status = ANY (ARRAY['succeeded', 'partial_succeeded', 'failed', 'replan_required']);

CREATE TABLE source_to_script_generation_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    generation_id UUID NOT NULL REFERENCES source_to_script_generations(id) ON DELETE CASCADE,
    source_chapter_id UUID,
    live_source_chapter_id UUID REFERENCES novel_chapters(id) ON DELETE SET NULL,
    item_key TEXT NOT NULL,
    manifest_ordinal INTEGER NOT NULL,
    source_revision BIGINT NOT NULL,
    source_content_hash TEXT NOT NULL,
    volume_index INTEGER,
    section_index INTEGER,
    volume_title TEXT,
    chapter_title TEXT NOT NULL,
    source_content TEXT,
    payload_purged_at TIMESTAMPTZ,
    is_target BOOLEAN NOT NULL DEFAULT false,
    base_episode_id UUID,
    base_episode_revision BIGINT,
    base_episode_content_hash TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT source_to_script_generation_items_ordinal_check CHECK (manifest_ordinal > 0),
    CONSTRAINT source_to_script_generation_items_revision_check CHECK (source_revision > 0),
    CONSTRAINT source_to_script_generation_items_hash_check CHECK (source_content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT source_to_script_generation_items_base_revision_check CHECK (
        base_episode_revision IS NULL OR base_episode_revision > 0
    ),
    CONSTRAINT source_to_script_generation_items_base_hash_check CHECK (
        base_episode_content_hash IS NULL OR base_episode_content_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT source_to_script_generation_items_live_identity_check CHECK (
        live_source_chapter_id IS NULL OR live_source_chapter_id = source_chapter_id
    ),
    CONSTRAINT source_to_script_generation_items_payload_check CHECK (
        (source_content IS NOT NULL AND payload_purged_at IS NULL)
        OR (source_content IS NULL AND payload_purged_at IS NOT NULL)
    ),
    CONSTRAINT source_to_script_generation_items_key_unique UNIQUE (generation_id, item_key),
    CONSTRAINT source_to_script_generation_items_ordinal_unique UNIQUE (generation_id, manifest_ordinal)
);

CREATE UNIQUE INDEX source_to_script_generation_items_chapter_unique
    ON source_to_script_generation_items(generation_id, source_chapter_id)
    WHERE source_chapter_id IS NOT NULL;

CREATE INDEX source_to_script_generation_items_target_idx
    ON source_to_script_generation_items(generation_id, is_target, manifest_ordinal);

CREATE TABLE script_episode_generation_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow_run_id UUID NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    generation_id UUID NOT NULL REFERENCES source_to_script_generations(id) ON DELETE CASCADE,
    attempt_generation INTEGER NOT NULL,
    source_id UUID NOT NULL,
    source_chapter_id UUID,
    item_key TEXT NOT NULL,
    status TEXT NOT NULL,
    episode_title TEXT,
    content TEXT,
    content_hash TEXT,
    error_code TEXT,
    error_message TEXT,
    prompt_template_key TEXT,
    prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE SET NULL,
    prompt_hash TEXT,
    prompt_source TEXT,
    provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    provider_model_id UUID REFERENCES provider_models(id) ON DELETE SET NULL,
    agent_run_id UUID REFERENCES agent_runs(id) ON DELETE SET NULL,
    provenance JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload_purged_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT script_episode_generation_results_attempt_check CHECK (attempt_generation > 0),
    CONSTRAINT script_episode_generation_results_status_check CHECK (status = ANY (ARRAY['succeeded', 'failed'])),
    CONSTRAINT script_episode_generation_results_content_hash_check CHECK (
        content_hash IS NULL OR content_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT script_episode_generation_results_workflow_item_unique UNIQUE (
        workflow_run_id, attempt_generation, item_key
    )
);

CREATE UNIQUE INDEX script_episode_generation_results_chapter_unique
    ON script_episode_generation_results(workflow_run_id, attempt_generation, source_chapter_id)
    WHERE source_chapter_id IS NOT NULL;

CREATE INDEX script_episode_generation_results_generation_status_idx
    ON script_episode_generation_results(generation_id, status, updated_at DESC);

ALTER TABLE script_episode_generation_results
    ADD CONSTRAINT script_episode_generation_results_terminal_payload_check CHECK (
        (
            status = 'succeeded'
            AND content_hash IS NOT NULL
            AND (
                (content IS NOT NULL AND payload_purged_at IS NULL)
                OR (content IS NULL AND payload_purged_at IS NOT NULL)
            )
        )
        OR (status = 'failed' AND error_code IS NOT NULL AND error_code <> '')
    );

ALTER TABLE script_versions
    ADD COLUMN source_to_script_generation_id UUID REFERENCES source_to_script_generations(id) ON DELETE SET NULL,
    ADD COLUMN generation_workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    ADD COLUMN generation_attempt_generation INTEGER,
    ADD CONSTRAINT script_versions_generation_attempt_check CHECK (
        generation_attempt_generation IS NULL OR generation_attempt_generation > 0
    );

CREATE UNIQUE INDEX script_versions_source_to_script_generation_unique
    ON script_versions(source_to_script_generation_id)
    WHERE source_to_script_generation_id IS NOT NULL;

ALTER TABLE script_episodes
    ADD COLUMN generation_result_id UUID REFERENCES script_episode_generation_results(id) ON DELETE SET NULL;

CREATE INDEX script_episodes_generation_result_idx
    ON script_episodes(generation_result_id)
    WHERE generation_result_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION protect_source_to_script_generation_snapshot()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF ROW(
        NEW.organization_id,
        NEW.project_id,
        NEW.workflow_run_id,
        NEW.root_generation_id,
        NEW.retry_of_generation_id,
        NEW.attempt_generation,
        NEW.source_id,
        NEW.source_type,
        NEW.source_revision,
        NEW.source_content_hash,
        NEW.source_snapshot_hash,
        NEW.script_id,
        NEW.expected_active_script_id,
        NEW.expected_current_version_id,
        NEW.expected_script_revision,
        NEW.base_script_version_id,
        NEW.prompt_template_key,
        NEW.prompt_version_id,
        NEW.prompt_content_hash,
        NEW.model_profile_key,
        NEW.provider_model_id,
        NEW.project_snapshot,
        NEW.manual_bindings,
        NEW.model_bindings,
        NEW.manifest,
        NEW.manifest_hash
    ) IS DISTINCT FROM ROW(
        OLD.organization_id,
        OLD.project_id,
        OLD.workflow_run_id,
        OLD.root_generation_id,
        OLD.retry_of_generation_id,
        OLD.attempt_generation,
        OLD.source_id,
        OLD.source_type,
        OLD.source_revision,
        OLD.source_content_hash,
        OLD.source_snapshot_hash,
        OLD.script_id,
        OLD.expected_active_script_id,
        OLD.expected_current_version_id,
        OLD.expected_script_revision,
        OLD.base_script_version_id,
        OLD.prompt_template_key,
        OLD.prompt_version_id,
        OLD.prompt_content_hash,
        OLD.model_profile_key,
        OLD.provider_model_id,
        OLD.project_snapshot,
        OLD.manual_bindings,
        OLD.model_bindings,
        OLD.manifest,
        OLD.manifest_hash
    ) THEN
        RAISE EXCEPTION 'source-to-script generation snapshot is immutable';
    END IF;
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER source_to_script_generations_protect_snapshot
BEFORE UPDATE ON source_to_script_generations
FOR EACH ROW EXECUTE FUNCTION protect_source_to_script_generation_snapshot();

-- +goose Down

SET search_path TO public;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM source_to_script_generations LIMIT 1) THEN
        RAISE EXCEPTION 'cannot roll back source-to-script generation provenance while records exist';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP INDEX IF EXISTS script_episodes_generation_result_idx;
ALTER TABLE script_episodes DROP COLUMN IF EXISTS generation_result_id;

DROP INDEX IF EXISTS script_versions_source_to_script_generation_unique;
ALTER TABLE script_versions
    DROP CONSTRAINT IF EXISTS script_versions_generation_attempt_check,
    DROP COLUMN IF EXISTS generation_attempt_generation,
    DROP COLUMN IF EXISTS generation_workflow_run_id,
    DROP COLUMN IF EXISTS source_to_script_generation_id;

DROP TRIGGER IF EXISTS source_to_script_generations_protect_snapshot ON source_to_script_generations;
DROP FUNCTION IF EXISTS protect_source_to_script_generation_snapshot();

DROP TABLE IF EXISTS script_episode_generation_results;
DROP TABLE IF EXISTS source_to_script_generation_items;
DROP TABLE IF EXISTS source_to_script_generations;

ALTER TABLE script_versions DROP CONSTRAINT IF EXISTS script_versions_status_check;
ALTER TABLE script_versions
    ADD CONSTRAINT script_versions_status_check CHECK (status = ANY (ARRAY['active', 'archived']));

DROP TRIGGER IF EXISTS scripts_maintain_revision ON scripts;
DROP FUNCTION IF EXISTS maintain_script_revision();
ALTER TABLE scripts
    DROP CONSTRAINT IF EXISTS scripts_revision_check,
    DROP COLUMN IF EXISTS revision;

DROP TRIGGER IF EXISTS novel_chapters_refresh_content_identity ON novel_chapters;
DROP FUNCTION IF EXISTS refresh_novel_chapter_content_identity();
ALTER TABLE novel_chapters
    DROP CONSTRAINT IF EXISTS novel_chapters_content_hash_check,
    DROP CONSTRAINT IF EXISTS novel_chapters_content_revision_check,
    DROP COLUMN IF EXISTS content_hash,
    DROP COLUMN IF EXISTS content_revision;

DROP TRIGGER IF EXISTS project_sources_refresh_content_identity ON project_sources;
DROP FUNCTION IF EXISTS refresh_project_source_content_identity();
ALTER TABLE project_sources
    DROP CONSTRAINT IF EXISTS project_sources_content_hash_check,
    DROP CONSTRAINT IF EXISTS project_sources_content_revision_check,
    DROP COLUMN IF EXISTS content_hash,
    DROP COLUMN IF EXISTS content_revision;
