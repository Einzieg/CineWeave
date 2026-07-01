CREATE TABLE IF NOT EXISTS review_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  workflow_run_id UUID NULL REFERENCES workflow_runs(id) ON DELETE SET NULL,
  review_type TEXT NOT NULL CHECK (review_type IN ('project', 'script', 'assets', 'storyboard', 'production', 'timeline', 'final_video')),
  status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
  summary JSONB NOT NULL DEFAULT '{}',
  input JSONB NOT NULL DEFAULT '{}',
  output JSONB NOT NULL DEFAULT '{}',
  provider_call_id UUID NULL REFERENCES provider_call_logs(id) ON DELETE SET NULL,
  prompt_version_id UUID NULL REFERENCES prompt_versions(id) ON DELETE SET NULL,
  prompt_hash TEXT NULL,
  error_code TEXT NULL,
  error_message TEXT NULL,
  created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ NULL,
  completed_at TIMESTAMPTZ NULL
);

CREATE TABLE IF NOT EXISTS review_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  review_run_id UUID NULL REFERENCES review_runs(id) ON DELETE SET NULL,
  item_type TEXT NOT NULL CHECK (item_type IN ('issue', 'warning', 'suggestion')),
  category TEXT NOT NULL CHECK (category IN ('script', 'asset', 'storyboard', 'shot_asset', 'shot_image', 'shot_video', 'timeline', 'final_video')),
  severity TEXT NOT NULL DEFAULT 'medium' CHECK (severity IN ('low', 'medium', 'high', 'critical')),
  title TEXT NOT NULL,
  description TEXT NOT NULL,
  suggestion TEXT NULL,
  entity_type TEXT NOT NULL CHECK (entity_type IN ('script_scene', 'canonical_asset', 'storyboard_shot', 'shot_asset_requirement', 'timeline_clip', 'final_video_version', 'project')),
  entity_id UUID NULL,
  related_entity_type TEXT NULL,
  related_entity_id UUID NULL,
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'ignored')),
  resolution_note TEXT NULL,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  resolved_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ NULL
);

DROP TRIGGER IF EXISTS review_items_set_updated_at ON review_items;
CREATE TRIGGER review_items_set_updated_at
BEFORE UPDATE ON review_items
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS idx_review_runs_project ON review_runs(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_review_runs_status ON review_runs(project_id, status);
CREATE INDEX IF NOT EXISTS idx_review_items_project ON review_items(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_review_items_status ON review_items(project_id, status);
CREATE INDEX IF NOT EXISTS idx_review_items_category ON review_items(project_id, category);
CREATE INDEX IF NOT EXISTS idx_review_items_entity ON review_items(entity_type, entity_id);

INSERT INTO prompt_templates(
  organization_id, template_key, name, description, purpose, modality, task_type, scope, status, is_system
)
VALUES (
  NULL,
  'project_review_agent',
  'Project Review Agent',
  'Review structured CineWeave project state and return actionable quality issues.',
  'project_review',
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

CREATE TEMP TABLE tmp_project_review_seed_prompts(template_key TEXT PRIMARY KEY, content TEXT NOT NULL) ON COMMIT DROP;

INSERT INTO tmp_project_review_seed_prompts(template_key, content)
VALUES (
  'project_review_agent',
  $prompt$You are the CineWeave project review agent.

Review the structured project context below and return only JSON.

Rules:
- Do not return markdown.
- Do not return explanations outside JSON.
- Return only valid JSON.
- Prefer issues that affect stable video production.
- Do not invent issues just to increase count.
- Use critical only when the production flow cannot continue.
- Use high when output quality is clearly affected.
- Use medium for recommended fixes.
- Use low for optional improvements.

Return this shape:
{
  "summary": {
    "overallStatus": "healthy|needs_attention|blocked",
    "criticalCount": 0,
    "highCount": 0,
    "mediumCount": 0,
    "lowCount": 0
  },
  "items": [
    {
      "itemType": "issue|warning|suggestion",
      "category": "script|asset|storyboard|shot_asset|shot_image|shot_video|timeline|final_video",
      "severity": "low|medium|high|critical",
      "title": "short issue title",
      "description": "concrete reason",
      "suggestion": "actionable fix",
      "entityType": "script_scene|canonical_asset|storyboard_shot|shot_asset_requirement|timeline_clip|final_video_version|project",
      "entityId": "uuid if known",
      "entityName": "human readable name if useful"
    }
  ]
}

Project:
{{ project.json }}

Deterministic checks already found:
{{ deterministic.json }}

Structured project context:
{{ context.json }}$prompt$
);

UPDATE prompt_versions pv
SET status = 'archived'
FROM prompt_templates t
JOIN tmp_project_review_seed_prompts p ON p.template_key = t.template_key
WHERE pv.template_id = t.id
  AND t.organization_id IS NULL
  AND t.template_key = 'project_review_agent'
  AND pv.status = 'active'
  AND pv.content_hash <> 'sha256:' || encode(digest(p.content, 'sha256'), 'hex');

INSERT INTO prompt_versions(
  prompt_template_id, template_id, version_no, version, status, title, content, content_format, variables_schema, metadata, content_hash, activated_at
)
SELECT t.id, t.id, COALESCE(MAX(v.version_no), 0) + 1, COALESCE(MAX(v.version), 0) + 1,
       'active', 'Project Review Agent v1', p.content, 'text', '{}'::jsonb, '{"seed":"project_review_center"}'::jsonb,
       'sha256:' || encode(digest(p.content, 'sha256'), 'hex'), now()
FROM tmp_project_review_seed_prompts p
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
JOIN tmp_project_review_seed_prompts p ON p.template_key = t.template_key
WHERE pv.template_id = t.id
  AND t.organization_id IS NULL
  AND t.template_key = 'project_review_agent'
  AND pv.content_hash = 'sha256:' || encode(digest(p.content, 'sha256'), 'hex')
  AND pv.id = (
    SELECT id
    FROM prompt_versions latest
    WHERE latest.template_id = t.id
      AND latest.content_hash = pv.content_hash
    ORDER BY latest.version_no DESC, latest.created_at DESC
    LIMIT 1
  );

INSERT INTO schema_migrations(version) VALUES ('000022_project_review_center')
ON CONFLICT (version) DO NOTHING;
