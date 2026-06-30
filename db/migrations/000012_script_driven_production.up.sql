ALTER TABLE projects
  ADD COLUMN IF NOT EXISTS content_type TEXT,
  ADD COLUMN IF NOT EXISTS video_ratio TEXT NOT NULL DEFAULT '16:9',
  ADD COLUMN IF NOT EXISTS art_style TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS director_manual TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS visual_manual TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS image_model_profile_key TEXT NOT NULL DEFAULT 'image_generation_default',
  ADD COLUMN IF NOT EXISTS video_model_profile_key TEXT NOT NULL DEFAULT 'video_generation_default',
  ADD COLUMN IF NOT EXISTS script_model_profile_key TEXT NOT NULL DEFAULT 'script_agent_default',
  ADD COLUMN IF NOT EXISTS image_quality TEXT NOT NULL DEFAULT 'standard',
  ADD COLUMN IF NOT EXISTS production_mode TEXT NOT NULL DEFAULT 'silent_video';

CREATE TABLE IF NOT EXISTS project_sources (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  source_type TEXT NOT NULL CHECK (source_type IN ('novel', 'script')),
  title TEXT NOT NULL,
  content TEXT NOT NULL,
  content_format TEXT NOT NULL DEFAULT 'plain_text' CHECK (content_format IN ('plain_text', 'markdown')),
  original_file_name TEXT,
  storage_key TEXT,
  status TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('ready', 'processing', 'processed', 'failed')),
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE novel_chapters
  ALTER COLUMN novel_id DROP NOT NULL,
  ADD COLUMN IF NOT EXISTS organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS source_id UUID REFERENCES project_sources(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS volume_title TEXT,
  ADD COLUMN IF NOT EXISTS chapter_title TEXT,
  ADD COLUMN IF NOT EXISTS content TEXT,
  ADD COLUMN IF NOT EXISTS event_state TEXT NOT NULL DEFAULT 'pending',
  ADD COLUMN IF NOT EXISTS event_summary JSONB,
  ADD COLUMN IF NOT EXISTS error_message TEXT,
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE UNIQUE INDEX IF NOT EXISTS novel_chapters_source_index_unique
  ON novel_chapters(source_id, chapter_index)
  WHERE source_id IS NOT NULL;

ALTER TABLE scripts
  ADD COLUMN IF NOT EXISTS source_id UUID REFERENCES project_sources(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'draft';

ALTER TABLE script_versions
  ADD COLUMN IF NOT EXISTS organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS version INTEGER,
  ADD COLUMN IF NOT EXISTS content TEXT,
  ADD COLUMN IF NOT EXISTS content_format TEXT NOT NULL DEFAULT 'markdown',
  ADD COLUMN IF NOT EXISTS source_type TEXT,
  ADD COLUMN IF NOT EXISTS prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS prompt_hash TEXT;

UPDATE script_versions
SET version = COALESCE(version, version_no)
WHERE version IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS script_versions_script_version_unique
  ON script_versions(script_id, version);

CREATE TABLE IF NOT EXISTS agent_sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_type TEXT NOT NULL CHECK (agent_type IN ('script_agent', 'asset_agent', 'storyboard_agent', 'shot_asset_agent')),
  title TEXT,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_messages (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  session_id UUID NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system', 'tool')),
  content TEXT NOT NULL,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  session_id UUID REFERENCES agent_sessions(id) ON DELETE SET NULL,
  agent_type TEXT NOT NULL,
  task_type TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
  input JSONB NOT NULL DEFAULT '{}',
  output JSONB NOT NULL DEFAULT '{}',
  provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
  prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE SET NULL,
  prompt_hash TEXT,
  error_code TEXT,
  error_message TEXT,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS canonical_assets (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  asset_type TEXT NOT NULL CHECK (asset_type IN ('character', 'scene', 'prop')),
  name TEXT NOT NULL,
  description TEXT NOT NULL,
  base_prompt TEXT,
  visual_traits JSONB NOT NULL DEFAULT '{}',
  reference_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  reference_media_file_id UUID REFERENCES media_files(id) ON DELETE SET NULL,
  reference_storage_key TEXT,
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'prompt_ready', 'image_running', 'image_succeeded', 'image_failed')),
  source_script_ids JSONB NOT NULL DEFAULT '[]',
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(project_id, asset_type, name)
);

CREATE TABLE IF NOT EXISTS asset_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  asset_id UUID NOT NULL REFERENCES canonical_assets(id) ON DELETE CASCADE,
  version INTEGER NOT NULL,
  description TEXT NOT NULL,
  base_prompt TEXT,
  visual_traits JSONB NOT NULL DEFAULT '{}',
  reference_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  reference_media_file_id UUID REFERENCES media_files(id) ON DELETE SET NULL,
  reference_storage_key TEXT,
  prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE SET NULL,
  prompt_hash TEXT,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(asset_id, version)
);

CREATE TABLE IF NOT EXISTS script_asset_links (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  script_id UUID NOT NULL REFERENCES scripts(id) ON DELETE CASCADE,
  asset_id UUID NOT NULL REFERENCES canonical_assets(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(script_id, asset_id)
);

ALTER TABLE storyboard_shots
  ADD COLUMN IF NOT EXISTS script_id UUID REFERENCES scripts(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS storyboard_source TEXT;

CREATE TABLE IF NOT EXISTS shot_asset_requirements (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE CASCADE,
  storyboard_shot_id UUID NOT NULL REFERENCES storyboard_shots(id) ON DELETE CASCADE,
  asset_id UUID NOT NULL REFERENCES canonical_assets(id) ON DELETE CASCADE,
  requirement_type TEXT NOT NULL,
  role_in_shot TEXT,
  costume TEXT,
  pose TEXT,
  expression TEXT,
  action TEXT,
  camera_relation TEXT,
  scene_state TEXT,
  prop_state TEXT,
  prompt TEXT,
  derived_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  derived_media_file_id UUID REFERENCES media_files(id) ON DELETE SET NULL,
  derived_storage_key TEXT,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'image_running', 'image_succeeded', 'image_failed', 'skipped')),
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS project_sources_set_updated_at ON project_sources;
CREATE TRIGGER project_sources_set_updated_at
BEFORE UPDATE ON project_sources
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS agent_sessions_set_updated_at ON agent_sessions;
CREATE TRIGGER agent_sessions_set_updated_at
BEFORE UPDATE ON agent_sessions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS canonical_assets_set_updated_at ON canonical_assets;
CREATE TRIGGER canonical_assets_set_updated_at
BEFORE UPDATE ON canonical_assets
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS shot_asset_requirements_set_updated_at ON shot_asset_requirements;
CREATE TRIGGER shot_asset_requirements_set_updated_at
BEFORE UPDATE ON shot_asset_requirements
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS project_sources_project_idx ON project_sources(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS scripts_project_status_idx ON scripts(project_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS script_versions_project_idx ON script_versions(project_id, script_id, version DESC);
CREATE INDEX IF NOT EXISTS agent_sessions_project_type_idx ON agent_sessions(project_id, agent_type, created_at DESC);
CREATE INDEX IF NOT EXISTS agent_messages_session_idx ON agent_messages(session_id, created_at);
CREATE INDEX IF NOT EXISTS agent_runs_project_type_idx ON agent_runs(project_id, agent_type, task_type, created_at DESC);
CREATE INDEX IF NOT EXISTS canonical_assets_project_type_idx ON canonical_assets(project_id, asset_type, created_at DESC);
CREATE INDEX IF NOT EXISTS script_asset_links_script_idx ON script_asset_links(script_id);
CREATE INDEX IF NOT EXISTS storyboard_shots_script_idx ON storyboard_shots(script_id, script_version_id);
CREATE INDEX IF NOT EXISTS shot_asset_requirements_shot_idx ON shot_asset_requirements(storyboard_shot_id);
CREATE INDEX IF NOT EXISTS shot_asset_requirements_asset_idx ON shot_asset_requirements(asset_id);

INSERT INTO permissions(permission_key, name, description) VALUES
  ('source.read', 'Source Read', 'Read project sources'),
  ('source.write', 'Source Write', 'Create or update project sources'),
  ('script.read', 'Script Read', 'Read scripts and script versions'),
  ('script.write', 'Script Write', 'Create or update scripts and script versions'),
  ('asset.analyze', 'Asset Analyze', 'Analyze scripts into canonical assets'),
  ('asset.generate', 'Asset Generate', 'Generate canonical and derived asset images'),
  ('storyboard.generate', 'Storyboard Generate', 'Generate storyboards from scripts')
ON CONFLICT (permission_key) DO UPDATE SET
  name = EXCLUDED.name,
  description = EXCLUDED.description;

WITH role_grants(role_key, permission_key) AS (
  VALUES
    ('project_owner', 'source.read'),
    ('project_owner', 'source.write'),
    ('project_owner', 'script.read'),
    ('project_owner', 'script.write'),
    ('project_owner', 'asset.analyze'),
    ('project_owner', 'asset.generate'),
    ('project_owner', 'storyboard.generate'),
    ('project_editor', 'source.read'),
    ('project_editor', 'source.write'),
    ('project_editor', 'script.read'),
    ('project_editor', 'script.write'),
    ('project_editor', 'asset.analyze'),
    ('project_editor', 'asset.generate'),
    ('project_editor', 'storyboard.generate'),
    ('project_viewer', 'source.read'),
    ('project_viewer', 'script.read')
)
INSERT INTO role_permissions(role_id, permission_key)
SELECT r.id, g.permission_key
FROM role_grants g
JOIN roles r ON r.organization_id IS NULL AND r.role_key = g.role_key
JOIN permissions p ON p.permission_key = g.permission_key
ON CONFLICT DO NOTHING;

WITH seed_prompts(template_key, name, description, purpose, modality, task_type, content) AS (
  VALUES
    ('script_agent_generate', 'Script Agent Generate', 'Generate a script from project source material.', 'script_agent_generate', 'text', 'text.generate', $prompt$You are CineWeave's script agent.

Project:
- Type: {{ project.projectType }}
- Content type: {{ project.contentType }}
- Art style: {{ project.artStyle }}
- Director manual: {{ project.directorManual }}

Source title: {{ source.title }}
Source type: {{ source.sourceType }}
Instruction: {{ input.instruction }}

Source content:
{{ source.content }}

Return only the script content. Use clear markdown scene structure.$prompt$),
    ('script_agent_rewrite', 'Script Agent Rewrite', 'Rewrite an existing script version.', 'script_agent_rewrite', 'text', 'text.generate', $prompt$You are CineWeave's script rewrite agent.

Project style: {{ project.artStyle }}
Director manual: {{ project.directorManual }}
Instruction: {{ input.instruction }}

Current script:
{{ script.content }}

Return only the rewritten script content.$prompt$),
    ('script_asset_extraction', 'Script Asset Extraction', 'Extract canonical characters, scenes, and props from a script.', 'script_asset_extraction', 'text', 'text.generate', $prompt$Extract canonical visual assets from this script.

Existing assets:
{{ assets.existing }}

Script:
{{ script.content }}

Return only JSON:
{
  "assets": [
    {
      "assetType": "character",
      "name": "...",
      "description": "...",
      "basePrompt": "...",
      "visualTraits": {}
    }
  ]
}

Rules:
- assetType must be one of character, scene, prop.
- Merge existing assets by stable name and type.
- Do not create character names for clothing, pose, or expressions.
- Scenes must be filmable spaces.
- Props must be visible objects that matter to the story.$prompt$),
    ('canonical_asset_image_prompt', 'Canonical Asset Image Prompt', 'Generate a reference image for a canonical asset.', 'canonical_asset_image_prompt', 'image', 'image.generate', $prompt$Create a clean reference image for this canonical asset.

Project art style: {{ project.artStyle }}
Director manual: {{ project.directorManual }}
Visual manual: {{ project.visualManual }}

Asset type: {{ asset.type }}
Asset name: {{ asset.name }}
Description: {{ asset.description }}
Base prompt: {{ asset.basePrompt }}
Visual traits: {{ asset.visualTraits }}

Rules:
- Single clear asset reference.
- No text, subtitles, watermarks, or UI.
- Keep the design reusable across many shots.$prompt$),
    ('storyboard_from_script', 'Storyboard From Script', 'Generate storyboard shots from an active script and assets.', 'storyboard_from_script', 'text', 'text.generate', $prompt$Create a production storyboard from this script and asset bible.

Project:
- Ratio: {{ project.videoRatio }}
- Art style: {{ project.artStyle }}
- Director manual: {{ project.directorManual }}

Script:
{{ script.content }}

Canonical assets:
{{ assets.items }}

Return only JSON:
{
  "title": "...",
  "summary": "...",
  "shots": [
    {
      "shotNo": 1,
      "duration": 5,
      "title": "...",
      "visual": "...",
      "camera": "...",
      "motion": "...",
      "mood": "...",
      "imagePrompt": "...",
      "videoPrompt": "...",
      "assetRequirements": [
        {
          "assetName": "...",
          "assetType": "character",
          "requirementType": "character_appearance",
          "roleInShot": "...",
          "costume": "...",
          "pose": "...",
          "expression": "...",
          "action": "...",
          "cameraRelation": "...",
          "sceneState": "...",
          "propState": "...",
          "prompt": "..."
        }
      ]
    }
  ]
}

Rules:
- Create 1 to {{ input.maxShots }} shots.
- Refer to assets by stable assetName and assetType.
- Return valid JSON only.$prompt$),
    ('shot_asset_requirement_analysis', 'Shot Asset Requirement Analysis', 'Analyze derived asset requirements per shot.', 'shot_asset_requirement_analysis', 'text', 'text.generate', $prompt$Analyze the current shot and list derived asset requirements.

Script:
{{ script.content }}

Shot:
{{ shot.summary }}

Canonical assets:
{{ assets.items }}

Return only JSON:
{
  "requirements": [
    {
      "assetName": "...",
      "assetType": "character",
      "requirementType": "character_appearance",
      "roleInShot": "...",
      "costume": "...",
      "pose": "...",
      "expression": "...",
      "action": "...",
      "cameraRelation": "...",
      "sceneState": "...",
      "propState": "...",
      "prompt": "..."
    }
  ]
}$prompt$),
    ('derived_asset_image_prompt', 'Derived Asset Image Prompt', 'Generate per-shot derived asset reference images.', 'derived_asset_image_prompt', 'image', 'image.generate', $prompt$Create a derived per-shot reference image.

Project art style: {{ project.artStyle }}
Director manual: {{ project.directorManual }}

Base asset:
- Name: {{ baseAsset.name }}
- Description: {{ baseAsset.description }}

Shot:
{{ shot.summary }}

Requirement:
{{ requirement.summary }}

Rules:
- Preserve identity from the base asset.
- Show only the current shot-specific variation.
- No text, subtitles, watermarks, or UI.$prompt$),
    ('shot_image_prompt', 'Shot Image Prompt', 'Generate shot image using canonical and derived asset context.', 'shot_image_prompt', 'image', 'image.generate', $prompt$Create a cinematic shot image for this production shot.

Project art style: {{ project.artStyle }}
Director manual: {{ project.directorManual }}
Visual manual: {{ project.visualManual }}
Video ratio: {{ project.videoRatio }}

Shot:
- Visual: {{ shot.visual }}
- Camera: {{ shot.camera }}
- Motion: {{ shot.motion }}
- Mood: {{ shot.mood }}
- Image prompt: {{ shot.imagePrompt }}

Asset context:
{{ assets.summary }}

Shot requirements:
{{ requirements.summary }}

Rules:
- Use the supplied reference images for consistency.
- No subtitles, captions, watermarks, speech bubbles, collage, or UI.$prompt$),
    ('shot_video_prompt', 'Shot Video Prompt', 'Generate shot video using shot image and asset requirements.', 'shot_video_prompt', 'video', 'video.create_task', $prompt$Create a {{ video.duration }}-second cinematic video from the reference shot image.

Shot:
- Visual: {{ shot.visual }}
- Camera: {{ shot.camera }}
- Motion: {{ shot.motion }}
- Mood: {{ shot.mood }}
- Video prompt: {{ shot.videoPrompt }}

Shot requirements:
{{ requirements.summary }}

Rules:
- Preserve the reference image composition and asset identity.
- Keep motion natural and cinematic.
- No subtitles, captions, title cards, watermarks, split-screen, collage, or extra characters.$prompt$)
)
INSERT INTO prompt_templates(
  organization_id, template_key, name, description, purpose, modality, task_type, scope, status, is_system
)
SELECT NULL, template_key, name, description, purpose, modality, task_type, 'system', 'active', true
FROM seed_prompts
ON CONFLICT DO NOTHING;

WITH seed_prompts(template_key, content) AS (
  VALUES
    ('script_agent_generate', $prompt$You are CineWeave's script agent.

Project:
- Type: {{ project.projectType }}
- Content type: {{ project.contentType }}
- Art style: {{ project.artStyle }}
- Director manual: {{ project.directorManual }}

Source title: {{ source.title }}
Source type: {{ source.sourceType }}
Instruction: {{ input.instruction }}

Source content:
{{ source.content }}

Return only the script content. Use clear markdown scene structure.$prompt$),
    ('script_agent_rewrite', $prompt$You are CineWeave's script rewrite agent.

Project style: {{ project.artStyle }}
Director manual: {{ project.directorManual }}
Instruction: {{ input.instruction }}

Current script:
{{ script.content }}

Return only the rewritten script content.$prompt$),
    ('script_asset_extraction', $prompt$Extract canonical visual assets from this script.

Existing assets:
{{ assets.existing }}

Script:
{{ script.content }}

Return only JSON:
{
  "assets": [
    {
      "assetType": "character",
      "name": "...",
      "description": "...",
      "basePrompt": "...",
      "visualTraits": {}
    }
  ]
}

Rules:
- assetType must be one of character, scene, prop.
- Merge existing assets by stable name and type.
- Do not create character names for clothing, pose, or expressions.
- Scenes must be filmable spaces.
- Props must be visible objects that matter to the story.$prompt$),
    ('canonical_asset_image_prompt', $prompt$Create a clean reference image for this canonical asset.

Project art style: {{ project.artStyle }}
Director manual: {{ project.directorManual }}
Visual manual: {{ project.visualManual }}

Asset type: {{ asset.type }}
Asset name: {{ asset.name }}
Description: {{ asset.description }}
Base prompt: {{ asset.basePrompt }}
Visual traits: {{ asset.visualTraits }}

Rules:
- Single clear asset reference.
- No text, subtitles, watermarks, or UI.
- Keep the design reusable across many shots.$prompt$),
    ('storyboard_from_script', $prompt$Create a production storyboard from this script and asset bible.

Project:
- Ratio: {{ project.videoRatio }}
- Art style: {{ project.artStyle }}
- Director manual: {{ project.directorManual }}

Script:
{{ script.content }}

Canonical assets:
{{ assets.items }}

Return only JSON:
{
  "title": "...",
  "summary": "...",
  "shots": [
    {
      "shotNo": 1,
      "duration": 5,
      "title": "...",
      "visual": "...",
      "camera": "...",
      "motion": "...",
      "mood": "...",
      "imagePrompt": "...",
      "videoPrompt": "...",
      "assetRequirements": [
        {
          "assetName": "...",
          "assetType": "character",
          "requirementType": "character_appearance",
          "roleInShot": "...",
          "costume": "...",
          "pose": "...",
          "expression": "...",
          "action": "...",
          "cameraRelation": "...",
          "sceneState": "...",
          "propState": "...",
          "prompt": "..."
        }
      ]
    }
  ]
}

Rules:
- Create 1 to {{ input.maxShots }} shots.
- Refer to assets by stable assetName and assetType.
- Return valid JSON only.$prompt$),
    ('shot_asset_requirement_analysis', $prompt$Analyze the current shot and list derived asset requirements.

Script:
{{ script.content }}

Shot:
{{ shot.summary }}

Canonical assets:
{{ assets.items }}

Return only JSON:
{
  "requirements": [
    {
      "assetName": "...",
      "assetType": "character",
      "requirementType": "character_appearance",
      "roleInShot": "...",
      "costume": "...",
      "pose": "...",
      "expression": "...",
      "action": "...",
      "cameraRelation": "...",
      "sceneState": "...",
      "propState": "...",
      "prompt": "..."
    }
  ]
}$prompt$),
    ('derived_asset_image_prompt', $prompt$Create a derived per-shot reference image.

Project art style: {{ project.artStyle }}
Director manual: {{ project.directorManual }}

Base asset:
- Name: {{ baseAsset.name }}
- Description: {{ baseAsset.description }}

Shot:
{{ shot.summary }}

Requirement:
{{ requirement.summary }}

Rules:
- Preserve identity from the base asset.
- Show only the current shot-specific variation.
- No text, subtitles, watermarks, or UI.$prompt$),
    ('shot_image_prompt', $prompt$Create a cinematic shot image for this production shot.

Project art style: {{ project.artStyle }}
Director manual: {{ project.directorManual }}
Visual manual: {{ project.visualManual }}
Video ratio: {{ project.videoRatio }}

Shot:
- Visual: {{ shot.visual }}
- Camera: {{ shot.camera }}
- Motion: {{ shot.motion }}
- Mood: {{ shot.mood }}
- Image prompt: {{ shot.imagePrompt }}

Asset context:
{{ assets.summary }}

Shot requirements:
{{ requirements.summary }}

Rules:
- Use the supplied reference images for consistency.
- No subtitles, captions, watermarks, speech bubbles, collage, or UI.$prompt$),
    ('shot_video_prompt', $prompt$Create a {{ video.duration }}-second cinematic video from the reference shot image.

Shot:
- Visual: {{ shot.visual }}
- Camera: {{ shot.camera }}
- Motion: {{ shot.motion }}
- Mood: {{ shot.mood }}
- Video prompt: {{ shot.videoPrompt }}

Shot requirements:
{{ requirements.summary }}

Rules:
- Preserve the reference image composition and asset identity.
- Keep motion natural and cinematic.
- No subtitles, captions, title cards, watermarks, split-screen, collage, or extra characters.$prompt$)
),
tmpl AS (
  SELECT pt.id, sp.template_key, sp.content
  FROM seed_prompts sp
  JOIN prompt_templates pt ON pt.organization_id IS NULL AND pt.template_key = sp.template_key
)
INSERT INTO prompt_versions(
  prompt_template_id, template_id, version_no, version, status, title, content, content_format, variables_schema, metadata, content_hash, activated_at
)
SELECT tmpl.id, tmpl.id, 1, 1, 'active', 'System v1', tmpl.content, 'text', '{}'::jsonb, '{"seed":"system"}'::jsonb,
       'sha256:' || encode(digest(tmpl.content, 'sha256'), 'hex'), now()
FROM tmpl
WHERE NOT EXISTS (SELECT 1 FROM prompt_versions WHERE template_id = tmpl.id);

INSERT INTO schema_migrations(version) VALUES ('000012_script_driven_production')
ON CONFLICT (version) DO NOTHING;
