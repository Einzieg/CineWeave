CREATE TABLE IF NOT EXISTS script_scenes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  script_id UUID NOT NULL REFERENCES scripts(id) ON DELETE CASCADE,
  script_version_id UUID NOT NULL REFERENCES script_versions(id) ON DELETE CASCADE,

  scene_index INTEGER NOT NULL,
  scene_no INTEGER NOT NULL,

  title TEXT NOT NULL,
  summary TEXT,

  location TEXT,
  time_of_day TEXT,
  atmosphere TEXT,

  characters JSONB NOT NULL DEFAULT '[]',
  scenes JSONB NOT NULL DEFAULT '[]',
  props JSONB NOT NULL DEFAULT '[]',

  action TEXT,
  dialogue TEXT,
  visual_goal TEXT,
  emotional_tone TEXT,
  conflict TEXT,
  outcome TEXT,

  source_event_ids JSONB NOT NULL DEFAULT '[]',

  content TEXT NOT NULL DEFAULT '',
  content_format TEXT NOT NULL DEFAULT 'markdown',

  review_status TEXT NOT NULL DEFAULT 'pending',
  manual_override BOOLEAN NOT NULL DEFAULT false,
  stale_state TEXT NOT NULL DEFAULT 'fresh',

  metadata JSONB NOT NULL DEFAULT '{}',

  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  edited_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  edited_at TIMESTAMPTZ,

  UNIQUE(script_version_id, scene_index)
);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'script_scenes_review_status_check'
  ) THEN
    ALTER TABLE script_scenes
      ADD CONSTRAINT script_scenes_review_status_check
      CHECK (review_status IN ('pending', 'approved', 'rejected', 'needs_edit'));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'script_scenes_stale_state_check'
  ) THEN
    ALTER TABLE script_scenes
      ADD CONSTRAINT script_scenes_stale_state_check
      CHECK (stale_state IN ('fresh', 'upstream_changed', 'needs_regeneration'));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'script_scenes_content_format_check'
  ) THEN
    ALTER TABLE script_scenes
      ADD CONSTRAINT script_scenes_content_format_check
      CHECK (content_format IN ('plain_text', 'markdown'));
  END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS scene_asset_links (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,

  script_scene_id UUID NOT NULL REFERENCES script_scenes(id) ON DELETE CASCADE,
  asset_id UUID NOT NULL REFERENCES canonical_assets(id) ON DELETE CASCADE,

  asset_role TEXT,
  usage_note TEXT,
  metadata JSONB NOT NULL DEFAULT '{}',

  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  UNIQUE(script_scene_id, asset_id)
);

ALTER TABLE storyboard_shots
  ADD COLUMN IF NOT EXISTS script_scene_id UUID NULL REFERENCES script_scenes(id) ON DELETE SET NULL;

DROP TRIGGER IF EXISTS script_scenes_set_updated_at ON script_scenes;
CREATE TRIGGER script_scenes_set_updated_at
BEFORE UPDATE ON script_scenes
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS idx_script_scenes_project
  ON script_scenes(project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_script_scenes_script
  ON script_scenes(script_id, scene_index);

CREATE INDEX IF NOT EXISTS idx_script_scenes_version
  ON script_scenes(script_version_id, scene_index);

CREATE INDEX IF NOT EXISTS idx_script_scenes_review
  ON script_scenes(project_id, review_status);

CREATE INDEX IF NOT EXISTS idx_scene_asset_links_scene
  ON scene_asset_links(script_scene_id);

CREATE INDEX IF NOT EXISTS idx_scene_asset_links_asset
  ON scene_asset_links(asset_id);

CREATE INDEX IF NOT EXISTS idx_storyboard_shots_script_scene
  ON storyboard_shots(script_scene_id);

WITH seed_prompts(template_key, name, description, purpose, modality, task_type, content) AS (
  VALUES
    ('script_scene_parser', 'Script Scene Parser', 'Parse free-form script text into structured script scenes.', 'script_scene_parser', 'text', 'text.generate', $prompt$You are CineWeave's script scene parser.

Project:
- Type: {{ project.projectType }}
- Content type: {{ project.contentType }}
- Art style: {{ project.artStyle }}
- Director manual: {{ project.directorManual }}
- Visual manual: {{ project.visualManual }}

Script title: {{ script.title }}
Script version id: {{ script.versionId }}

Script:
{{ script.content }}

Return only valid JSON:
{
  "scenes": [
    {
      "sceneNo": 1,
      "title": "...",
      "summary": "...",
      "location": "...",
      "timeOfDay": "...",
      "atmosphere": "...",
      "characters": ["..."],
      "scenes": ["..."],
      "props": ["..."],
      "action": "...",
      "dialogue": "...",
      "visualGoal": "...",
      "emotionalTone": "...",
      "conflict": "...",
      "outcome": "...",
      "sourceEventIds": [],
      "content": "## Scene 1: ..."
    }
  ]
}

Rules:
- Return legal JSON only.
- Do not wrap the answer in markdown fences.
- Every scene must be usable for downstream storyboard generation.
- Use stable names for characters, locations, and props.
- Do not turn clothing, posture, or age state into character names.
- Preserve explicit scene structure when the script already has scenes.
- If the script has no scene structure, split by story beats.$prompt$),
    ('script_scene_rewrite', 'Script Scene Rewrite', 'Rewrite a single structured script scene.', 'script_scene_rewrite', 'text', 'text.generate', $prompt$You are CineWeave's script scene rewrite agent.

Project:
- Type: {{ project.projectType }}
- Content type: {{ project.contentType }}
- Art style: {{ project.artStyle }}
- Director manual: {{ project.directorManual }}
- Visual manual: {{ project.visualManual }}

Original scene:
{{ scene.content }}

User instruction:
{{ input.instruction }}

Related assets:
{{ assets.items }}

Related events:
{{ events.items }}

Return only one valid scene JSON object with the same fields as script_scene_parser scenes.$prompt$)
)
INSERT INTO prompt_templates(
  organization_id, template_key, name, description, purpose, modality, task_type, scope, status, is_system
)
SELECT NULL, template_key, name, description, purpose, modality, task_type, 'system', 'active', true
FROM seed_prompts
ON CONFLICT DO NOTHING;

WITH seed_prompts(template_key, content) AS (
  VALUES
    ('script_scene_parser', $prompt$You are CineWeave's script scene parser.

Project:
- Type: {{ project.projectType }}
- Content type: {{ project.contentType }}
- Art style: {{ project.artStyle }}
- Director manual: {{ project.directorManual }}
- Visual manual: {{ project.visualManual }}

Script title: {{ script.title }}
Script version id: {{ script.versionId }}

Script:
{{ script.content }}

Return only valid JSON:
{
  "scenes": [
    {
      "sceneNo": 1,
      "title": "...",
      "summary": "...",
      "location": "...",
      "timeOfDay": "...",
      "atmosphere": "...",
      "characters": ["..."],
      "scenes": ["..."],
      "props": ["..."],
      "action": "...",
      "dialogue": "...",
      "visualGoal": "...",
      "emotionalTone": "...",
      "conflict": "...",
      "outcome": "...",
      "sourceEventIds": [],
      "content": "## Scene 1: ..."
    }
  ]
}

Rules:
- Return legal JSON only.
- Do not wrap the answer in markdown fences.
- Every scene must be usable for downstream storyboard generation.
- Use stable names for characters, locations, and props.
- Do not turn clothing, posture, or age state into character names.
- Preserve explicit scene structure when the script already has scenes.
- If the script has no scene structure, split by story beats.$prompt$),
    ('script_scene_rewrite', $prompt$You are CineWeave's script scene rewrite agent.

Project:
- Type: {{ project.projectType }}
- Content type: {{ project.contentType }}
- Art style: {{ project.artStyle }}
- Director manual: {{ project.directorManual }}
- Visual manual: {{ project.visualManual }}

Original scene:
{{ scene.content }}

User instruction:
{{ input.instruction }}

Related assets:
{{ assets.items }}

Related events:
{{ events.items }}

Return only one valid scene JSON object with the same fields as script_scene_parser scenes.$prompt$)
)
INSERT INTO prompt_versions(
  prompt_template_id, template_id, version_no, version, status, title, content, content_format, variables_schema, metadata, content_hash, activated_at
)
SELECT t.id, t.id, 1, 1, 'active', 'System v1', p.content, 'text', '{}'::jsonb, '{"seed":"system"}'::jsonb,
       'sha256:' || encode(digest(p.content, 'sha256'), 'hex'), now()
FROM seed_prompts p
JOIN prompt_templates t ON t.organization_id IS NULL AND t.template_key = p.template_key
WHERE NOT EXISTS (SELECT 1 FROM prompt_versions WHERE template_id = t.id);

INSERT INTO schema_migrations(version) VALUES ('000016_script_scenes')
ON CONFLICT (version) DO NOTHING;
