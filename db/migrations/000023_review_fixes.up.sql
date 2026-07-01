ALTER TABLE review_items
  DROP CONSTRAINT IF EXISTS review_items_entity_type_check;

ALTER TABLE review_items
  ADD CONSTRAINT review_items_entity_type_check
  CHECK (entity_type IN ('script_scene', 'canonical_asset', 'storyboard_shot', 'shot_asset_requirement', 'timeline_clip', 'project_timeline', 'final_video_version', 'project'));

ALTER TABLE project_timelines
  ADD COLUMN IF NOT EXISTS manual_override BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS stale_state TEXT NOT NULL DEFAULT 'fresh',
  ADD COLUMN IF NOT EXISTS edited_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ NULL;

ALTER TABLE timeline_clips
  ADD COLUMN IF NOT EXISTS manual_override BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS stale_state TEXT NOT NULL DEFAULT 'fresh',
  ADD COLUMN IF NOT EXISTS edited_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ NULL;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'project_timelines_stale_state_check'
  ) THEN
    ALTER TABLE project_timelines
      ADD CONSTRAINT project_timelines_stale_state_check
      CHECK (stale_state IN ('fresh', 'upstream_changed', 'needs_regeneration'));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'timeline_clips_stale_state_check'
  ) THEN
    ALTER TABLE timeline_clips
      ADD CONSTRAINT timeline_clips_stale_state_check
      CHECK (stale_state IN ('fresh', 'upstream_changed', 'needs_regeneration'));
  END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS review_fixes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  review_item_id UUID NOT NULL REFERENCES review_items(id) ON DELETE CASCADE,
  target_entity_type TEXT NOT NULL,
  target_entity_id UUID NULL,
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'applied', 'dismissed', 'failed')),
  fix_type TEXT NOT NULL DEFAULT 'patch' CHECK (fix_type IN ('patch', 'regenerate', 'navigate', 'note')),
  title TEXT NOT NULL,
  explanation TEXT NOT NULL,
  before_snapshot JSONB NOT NULL DEFAULT '{}',
  patch JSONB NOT NULL DEFAULT '{}',
  after_preview JSONB NOT NULL DEFAULT '{}',
  regenerate_request JSONB NULL,
  prompt_version_id UUID NULL REFERENCES prompt_versions(id) ON DELETE SET NULL,
  prompt_hash TEXT NULL,
  provider_call_id UUID NULL REFERENCES provider_call_logs(id) ON DELETE SET NULL,
  error_code TEXT NULL,
  error_message TEXT NULL,
  created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  applied_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  applied_at TIMESTAMPTZ NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS review_fixes_set_updated_at ON review_fixes;
CREATE TRIGGER review_fixes_set_updated_at
BEFORE UPDATE ON review_fixes
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS idx_review_fixes_item ON review_fixes(review_item_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_review_fixes_project ON review_fixes(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_review_fixes_status ON review_fixes(project_id, status);
CREATE INDEX IF NOT EXISTS project_timelines_project_stale_state_idx ON project_timelines(project_id, stale_state);
CREATE INDEX IF NOT EXISTS timeline_clips_project_stale_state_idx ON timeline_clips(project_id, stale_state);

INSERT INTO prompt_templates(
  organization_id, template_key, name, description, purpose, modality, task_type, scope, status, is_system
)
VALUES (
  NULL,
  'review_fix_agent',
  'Review Fix Agent',
  'Generate draft JSON patch suggestions for review items.',
  'review_fix',
  'text',
  'text.generate',
  'system',
  'active',
  true
)
ON CONFLICT (template_key) WHERE organization_id IS NULL DO UPDATE
SET name = EXCLUDED.name,
    description = EXCLUDED.description,
    purpose = EXCLUDED.purpose,
    modality = EXCLUDED.modality,
    task_type = EXCLUDED.task_type,
    scope = EXCLUDED.scope,
    status = 'active',
    is_system = true,
    updated_at = now();

CREATE TEMP TABLE tmp_review_fix_seed_prompts(template_key TEXT PRIMARY KEY, content TEXT NOT NULL) ON COMMIT DROP;

INSERT INTO tmp_review_fix_seed_prompts(template_key, content)
VALUES (
  'review_fix_agent',
  $prompt$You are the CineWeave review fix agent.

Generate a conservative draft JSON patch suggestion for a review item.

Rules:
- Return JSON only.
- Do not return markdown.
- Do not invent entities that are not present in the target snapshot.
- The patch can only include editable fields listed below.
- Do not overwrite explicit manual user edits unless the review item requires it.
- Keep the patch small and focused.
- If the issue cannot be safely fixed automatically, return fixType "note" with an empty patch.

Return this shape:
{
  "fixType": "patch|regenerate|navigate|note",
  "title": "short fix title",
  "explanation": "why this fix is safe",
  "patch": {},
  "regenerate": {
    "recommended": false,
    "targetType": "",
    "targetId": "",
    "reason": ""
  }
}

Review item:
{{ reviewItem.json }}

Target entity type:
{{ target.entityType }}

Editable fields:
{{ target.editableFields }}

Target snapshot:
{{ target.snapshot }}

Project context:
{{ project.json }}

User instruction:
{{ input.instruction }}$prompt$
);

UPDATE prompt_versions pv
SET status = 'archived'
FROM prompt_templates t
JOIN tmp_review_fix_seed_prompts p ON p.template_key = t.template_key
WHERE pv.template_id = t.id
  AND t.organization_id IS NULL
  AND t.template_key = 'review_fix_agent'
  AND pv.status = 'active'
  AND pv.content_hash <> 'sha256:' || encode(digest(p.content, 'sha256'), 'hex');

INSERT INTO prompt_versions(
  prompt_template_id, template_id, version_no, version, status, title, content, content_format, variables_schema, metadata, content_hash, activated_at
)
SELECT t.id, t.id, COALESCE(MAX(v.version_no), 0) + 1, COALESCE(MAX(v.version), 0) + 1,
       'active', 'Review Fix Agent v1', p.content, 'text', '{}'::jsonb, '{"seed":"review_fix_suggestions"}'::jsonb,
       'sha256:' || encode(digest(p.content, 'sha256'), 'hex'), now()
FROM tmp_review_fix_seed_prompts p
JOIN prompt_templates t ON t.organization_id IS NULL AND t.template_key = p.template_key
LEFT JOIN prompt_versions v ON v.template_id = t.id
WHERE NOT EXISTS (
  SELECT 1
  FROM prompt_versions existing
  WHERE existing.template_id = t.id
    AND existing.content_hash = 'sha256:' || encode(digest(p.content, 'sha256'), 'hex')
)
GROUP BY t.id, p.content;

UPDATE prompt_versions pv
SET status = 'active',
    activated_at = COALESCE(activated_at, now())
FROM prompt_templates t
JOIN tmp_review_fix_seed_prompts p ON p.template_key = t.template_key
WHERE pv.template_id = t.id
  AND t.organization_id IS NULL
  AND t.template_key = 'review_fix_agent'
  AND pv.content_hash = 'sha256:' || encode(digest(p.content, 'sha256'), 'hex')
  AND pv.id = (
    SELECT id
    FROM prompt_versions latest
    WHERE latest.template_id = t.id
      AND latest.content_hash = pv.content_hash
    ORDER BY latest.version_no DESC, latest.created_at DESC
    LIMIT 1
  );

INSERT INTO schema_migrations(version) VALUES ('000023_review_fixes')
ON CONFLICT (version) DO NOTHING;
