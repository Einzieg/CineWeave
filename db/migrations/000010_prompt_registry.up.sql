ALTER TABLE prompt_templates
  ADD COLUMN IF NOT EXISTS description TEXT,
  ADD COLUMN IF NOT EXISTS modality TEXT,
  ADD COLUMN IF NOT EXISTS task_type TEXT,
  ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'system',
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active',
  ADD COLUMN IF NOT EXISTS is_system BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE prompt_templates
SET
  modality = COALESCE(modality, 'text'),
  task_type = COALESCE(task_type, 'text.generate'),
  scope = CASE WHEN organization_id IS NULL THEN 'system' ELSE 'organization' END,
  is_system = organization_id IS NULL
WHERE modality IS NULL
   OR task_type IS NULL
   OR scope IS NULL
   OR is_system IS NULL;

ALTER TABLE prompt_templates
  ALTER COLUMN modality SET NOT NULL,
  ALTER COLUMN task_type SET NOT NULL;

ALTER TABLE prompt_templates
  DROP CONSTRAINT IF EXISTS prompt_templates_scope_check,
  DROP CONSTRAINT IF EXISTS prompt_templates_status_check;

ALTER TABLE prompt_templates
  ADD CONSTRAINT prompt_templates_scope_check CHECK (scope IN ('system', 'organization')),
  ADD CONSTRAINT prompt_templates_status_check CHECK (status IN ('active', 'archived'));

ALTER TABLE prompt_versions
  ADD COLUMN IF NOT EXISTS template_id UUID,
  ADD COLUMN IF NOT EXISTS version INTEGER,
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'draft',
  ADD COLUMN IF NOT EXISTS title TEXT,
  ADD COLUMN IF NOT EXISTS content_format TEXT NOT NULL DEFAULT 'text',
  ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS activated_at TIMESTAMPTZ;

UPDATE prompt_versions
SET
  template_id = COALESCE(template_id, prompt_template_id),
  version = COALESCE(version, version_no)
WHERE template_id IS NULL
   OR version IS NULL;

WITH ranked AS (
  SELECT
    id,
    row_number() OVER (
      PARTITION BY COALESCE(template_id, prompt_template_id)
      ORDER BY COALESCE(version, version_no) DESC
    ) AS rank_no
  FROM prompt_versions
)
UPDATE prompt_versions pv
SET
  status = CASE WHEN ranked.rank_no = 1 THEN 'active' ELSE 'archived' END,
  activated_at = CASE WHEN ranked.rank_no = 1 THEN COALESCE(pv.activated_at, pv.created_at) ELSE pv.activated_at END
FROM ranked
WHERE pv.id = ranked.id
  AND pv.status = 'draft';

ALTER TABLE prompt_versions
  ALTER COLUMN template_id SET NOT NULL,
  ALTER COLUMN version SET NOT NULL;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'prompt_versions_template_id_fkey'
      AND conrelid = 'prompt_versions'::regclass
  ) THEN
    ALTER TABLE prompt_versions
      ADD CONSTRAINT prompt_versions_template_id_fkey
      FOREIGN KEY (template_id) REFERENCES prompt_templates(id) ON DELETE CASCADE;
  END IF;
END $$;

ALTER TABLE prompt_versions
  DROP CONSTRAINT IF EXISTS prompt_versions_status_check,
  DROP CONSTRAINT IF EXISTS prompt_versions_content_format_check;

ALTER TABLE prompt_versions
  ADD CONSTRAINT prompt_versions_status_check CHECK (status IN ('draft', 'active', 'archived')),
  ADD CONSTRAINT prompt_versions_content_format_check CHECK (content_format IN ('text', 'markdown'));

CREATE UNIQUE INDEX IF NOT EXISTS idx_prompt_templates_system_key
  ON prompt_templates(template_key)
  WHERE organization_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_prompt_versions_template_version
  ON prompt_versions(template_id, version);

CREATE UNIQUE INDEX IF NOT EXISTS idx_prompt_versions_one_active
  ON prompt_versions(template_id)
  WHERE status = 'active';

CREATE TABLE IF NOT EXISTS prompt_bindings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
  template_key TEXT NOT NULL,
  prompt_version_id UUID NOT NULL REFERENCES prompt_versions(id) ON DELETE RESTRICT,
  status TEXT NOT NULL DEFAULT 'active',
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT prompt_bindings_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE INDEX IF NOT EXISTS idx_prompt_bindings_org_key
  ON prompt_bindings(organization_id, template_key, status);

CREATE INDEX IF NOT EXISTS idx_prompt_bindings_project_key
  ON prompt_bindings(project_id, template_key, status);

CREATE UNIQUE INDEX IF NOT EXISTS idx_prompt_bindings_active_org
  ON prompt_bindings(organization_id, template_key)
  WHERE project_id IS NULL AND status = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS idx_prompt_bindings_active_project
  ON prompt_bindings(project_id, template_key)
  WHERE project_id IS NOT NULL AND status = 'active';

DROP TRIGGER IF EXISTS prompt_templates_set_updated_at ON prompt_templates;
CREATE TRIGGER prompt_templates_set_updated_at
BEFORE UPDATE ON prompt_templates
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS prompt_bindings_set_updated_at ON prompt_bindings;
CREATE TRIGGER prompt_bindings_set_updated_at
BEFORE UPDATE ON prompt_bindings
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

INSERT INTO permissions(permission_key, name, description) VALUES
  ('prompt.read', 'Prompt Read', 'Read prompt templates, versions, and bindings'),
  ('prompt.manage', 'Prompt Manage', 'Create and activate prompt versions and bindings')
ON CONFLICT (permission_key) DO UPDATE SET
  name = EXCLUDED.name,
  description = EXCLUDED.description;

WITH role_grants(role_key, permission_key) AS (
  VALUES
    ('org_admin', 'prompt.read'),
    ('org_admin', 'prompt.manage'),
    ('organization_admin', 'prompt.read'),
    ('organization_admin', 'prompt.manage'),
    ('project_owner', 'prompt.read'),
    ('project_owner', 'prompt.manage'),
    ('project_editor', 'prompt.read'),
    ('project_viewer', 'prompt.read')
)
INSERT INTO role_permissions(role_id, permission_key)
SELECT r.id, g.permission_key
FROM role_grants g
JOIN roles r ON r.organization_id IS NULL AND r.role_key = g.role_key
JOIN permissions p ON p.permission_key = g.permission_key
ON CONFLICT DO NOTHING;

INSERT INTO prompt_templates(
  organization_id, template_key, name, description, purpose, modality, task_type, scope, status, is_system
)
SELECT NULL, 'storyboard_planner', 'Storyboard Planner', 'Default text prompt for storyboard planning.', 'storyboard_planner', 'text', 'text.generate', 'system', 'active', true
WHERE NOT EXISTS (
  SELECT 1 FROM prompt_templates WHERE organization_id IS NULL AND template_key = 'storyboard_planner'
);

UPDATE prompt_templates
SET
  name = 'Storyboard Planner',
  description = 'Default text prompt for storyboard planning.',
  purpose = 'storyboard_planner',
  modality = 'text',
  task_type = 'text.generate',
  scope = 'system',
  status = 'active',
  is_system = true
WHERE organization_id IS NULL AND template_key = 'storyboard_planner';

WITH tmpl AS (
  SELECT id FROM prompt_templates WHERE organization_id IS NULL AND template_key = 'storyboard_planner' ORDER BY created_at LIMIT 1
),
content AS (
  SELECT $prompt$You are CineWeave's storyboard planner.

Convert the user's idea into a short storyboard JSON.

User idea:
{{ input.prompt }}

Project aspect ratio:
{{ project.aspectRatio }}

Return only JSON:
{
  "title": "...",
  "summary": "...",
  "shots": [
    {
      "shotNo": 1,
      "duration": 3,
      "visual": "...",
      "camera": "...",
      "motion": "...",
      "mood": "...",
      "imagePrompt": "...",
      "videoPrompt": "..."
    }
  ]
}

Rules:
- Return valid JSON only.
- Create 1 to 3 shots.
- Keep descriptions concise and cinematic.
- Do not include markdown fences.$prompt$::text AS value
)
INSERT INTO prompt_versions(
  prompt_template_id, template_id, version_no, version, status, title, content, content_format, variables_schema, metadata, content_hash, activated_at
)
SELECT tmpl.id, tmpl.id, 1, 1, 'active', 'System v1', content.value, 'text', '{}'::jsonb, '{"seed":"system"}'::jsonb,
       'sha256:' || encode(digest(content.value, 'sha256'), 'hex'), now()
FROM tmpl, content
WHERE NOT EXISTS (SELECT 1 FROM prompt_versions WHERE template_id = tmpl.id);

INSERT INTO prompt_templates(
  organization_id, template_key, name, description, purpose, modality, task_type, scope, status, is_system
)
SELECT NULL, 'storyboard_image_prompt', 'Storyboard Image Prompt', 'Default image generation prompt for storyboard shots.', 'image_prompt', 'image', 'image.generate', 'system', 'active', true
WHERE NOT EXISTS (
  SELECT 1 FROM prompt_templates WHERE organization_id IS NULL AND template_key = 'storyboard_image_prompt'
);

UPDATE prompt_templates
SET
  name = 'Storyboard Image Prompt',
  description = 'Default image generation prompt for storyboard shots.',
  purpose = 'image_prompt',
  modality = 'image',
  task_type = 'image.generate',
  scope = 'system',
  status = 'active',
  is_system = true
WHERE organization_id IS NULL AND template_key = 'storyboard_image_prompt';

WITH tmpl AS (
  SELECT id FROM prompt_templates WHERE organization_id IS NULL AND template_key = 'storyboard_image_prompt' ORDER BY created_at LIMIT 1
),
content AS (
  SELECT $prompt$Create a cinematic storyboard image for this shot.

Project idea:
{{ input.prompt }}

Shot visual:
{{ shot.visual }}

Camera:
{{ shot.camera }}

Mood:
{{ shot.mood }}

Image prompt:
{{ shot.imagePrompt }}

Style rules:
- High detail.
- Strong composition.
- No subtitles.
- No watermarks.
- No speech bubbles.$prompt$::text AS value
)
INSERT INTO prompt_versions(
  prompt_template_id, template_id, version_no, version, status, title, content, content_format, variables_schema, metadata, content_hash, activated_at
)
SELECT tmpl.id, tmpl.id, 1, 1, 'active', 'System v1', content.value, 'text', '{}'::jsonb, '{"seed":"system"}'::jsonb,
       'sha256:' || encode(digest(content.value, 'sha256'), 'hex'), now()
FROM tmpl, content
WHERE NOT EXISTS (SELECT 1 FROM prompt_versions WHERE template_id = tmpl.id);

INSERT INTO prompt_templates(
  organization_id, template_key, name, description, purpose, modality, task_type, scope, status, is_system
)
SELECT NULL, 'storyboard_video_prompt', 'Storyboard Video Prompt', 'Default video generation prompt for storyboard shots.', 'video_prompt', 'video', 'video.create_task', 'system', 'active', true
WHERE NOT EXISTS (
  SELECT 1 FROM prompt_templates WHERE organization_id IS NULL AND template_key = 'storyboard_video_prompt'
);

UPDATE prompt_templates
SET
  name = 'Storyboard Video Prompt',
  description = 'Default video generation prompt for storyboard shots.',
  purpose = 'video_prompt',
  modality = 'video',
  task_type = 'video.create_task',
  scope = 'system',
  status = 'active',
  is_system = true
WHERE organization_id IS NULL AND template_key = 'storyboard_video_prompt';

WITH tmpl AS (
  SELECT id FROM prompt_templates WHERE organization_id IS NULL AND template_key = 'storyboard_video_prompt' ORDER BY created_at LIMIT 1
),
content AS (
  SELECT $prompt$Create a {{ video.duration }}-second cinematic video based on the reference image.

Project idea:
{{ input.prompt }}

Shot visual:
{{ shot.visual }}

Camera:
{{ shot.camera }}

Motion:
{{ shot.motion }}

Mood:
{{ shot.mood }}

Video prompt:
{{ shot.videoPrompt }}

Rules:
- Keep the same character, scene layout, lighting, art style, and composition as the reference image.
- Use the reference image as consistency guidance, not as a collage.
- Do not add subtitles, captions, title cards, watermarks, split-screen, collage, or extra characters.
- Keep motion natural and cinematic.$prompt$::text AS value
)
INSERT INTO prompt_versions(
  prompt_template_id, template_id, version_no, version, status, title, content, content_format, variables_schema, metadata, content_hash, activated_at
)
SELECT tmpl.id, tmpl.id, 1, 1, 'active', 'System v1', content.value, 'text', '{}'::jsonb, '{"seed":"system"}'::jsonb,
       'sha256:' || encode(digest(content.value, 'sha256'), 'hex'), now()
FROM tmpl, content
WHERE NOT EXISTS (SELECT 1 FROM prompt_versions WHERE template_id = tmpl.id);

INSERT INTO schema_migrations(version) VALUES ('000010_prompt_registry')
ON CONFLICT (version) DO NOTHING;
