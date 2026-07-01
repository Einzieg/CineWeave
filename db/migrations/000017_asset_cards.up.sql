ALTER TABLE canonical_assets
  ADD COLUMN IF NOT EXISTS profile JSONB NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS negative_prompt TEXT NULL,
  ADD COLUMN IF NOT EXISTS consistency_prompt TEXT NULL,
  ADD COLUMN IF NOT EXISTS primary_reference_artifact_id UUID NULL REFERENCES artifacts(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS primary_reference_media_file_id UUID NULL REFERENCES media_files(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS primary_reference_storage_key TEXT NULL,
  ADD COLUMN IF NOT EXISTS lock_reference BOOLEAN NOT NULL DEFAULT false;

UPDATE canonical_assets
SET primary_reference_artifact_id = COALESCE(primary_reference_artifact_id, reference_artifact_id),
    primary_reference_media_file_id = COALESCE(primary_reference_media_file_id, reference_media_file_id),
    primary_reference_storage_key = COALESCE(primary_reference_storage_key, reference_storage_key)
WHERE primary_reference_artifact_id IS NULL
   OR primary_reference_media_file_id IS NULL
   OR COALESCE(primary_reference_storage_key, '') = '';

CREATE TABLE IF NOT EXISTS asset_references (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  asset_id UUID NOT NULL REFERENCES canonical_assets(id) ON DELETE CASCADE,

  reference_type TEXT NOT NULL DEFAULT 'generated',
  title TEXT NULL,
  description TEXT NULL,

  artifact_id UUID NULL REFERENCES artifacts(id) ON DELETE SET NULL,
  media_file_id UUID NULL REFERENCES media_files(id) ON DELETE SET NULL,
  storage_key TEXT NULL,
  preview_url TEXT NULL,

  prompt TEXT NULL,
  prompt_version_id UUID NULL REFERENCES prompt_versions(id) ON DELETE SET NULL,
  prompt_hash TEXT NULL,

  is_primary BOOLEAN NOT NULL DEFAULT false,
  status TEXT NOT NULL DEFAULT 'ready',

  metadata JSONB NOT NULL DEFAULT '{}',

  created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'asset_references_reference_type_check'
  ) THEN
    ALTER TABLE asset_references
      ADD CONSTRAINT asset_references_reference_type_check
      CHECK (reference_type IN ('generated', 'uploaded', 'derived', 'selected'));
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'asset_references_status_check'
  ) THEN
    ALTER TABLE asset_references
      ADD CONSTRAINT asset_references_status_check
      CHECK (status IN ('ready', 'archived', 'failed'));
  END IF;
END;
$$;

DROP TRIGGER IF EXISTS asset_references_set_updated_at ON asset_references;
CREATE TRIGGER asset_references_set_updated_at
BEFORE UPDATE ON asset_references
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS idx_asset_references_asset
  ON asset_references(asset_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_asset_references_project
  ON asset_references(project_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_asset_references_one_primary
  ON asset_references(asset_id)
  WHERE is_primary = true AND status = 'ready';

WITH seed_prompts(template_key, name, description, purpose, modality, task_type, content) AS (
  VALUES
    ('asset_card_generation', 'Asset Card Generation', 'Generate structured reusable asset cards for canonical assets.', 'asset_card_generation', 'text', 'text.generate', $prompt$You are CineWeave's asset card designer.

Create a structured reusable asset card for the canonical asset.

Project:
{{ project }}

Asset:
{{ asset }}

Related script scenes:
{{ scenes }}

Return only valid JSON:
{
  "profile": {},
  "basePrompt": "prompt for generating the asset reference image",
  "consistencyPrompt": "stable identity and continuity prompt for later shots",
  "negativePrompt": "things that must not appear"
}

Rules:
- The profile object must contain stable visual fields appropriate for the asset type.
- Characters should include appearance, age range, body type, baseline costume, personality, visual keywords, and forbidden changes when known.
- Scenes should include space type, era, color palette, key elements, atmosphere, and forbidden changes when known.
- Props should include category, material, condition, size, key features, and forbidden changes when known.
- Keep names stable and avoid turning costumes, poses, or age states into character names.
- Do not include markdown fences or explanatory text.$prompt$),
    ('canonical_asset_image_prompt', 'Canonical Asset Image Prompt', 'Generate a reference image for a canonical asset using the asset card.', 'canonical_asset_image_prompt', 'image', 'image.generate', $prompt$Create a clean reusable reference image for this canonical asset.

Asset:
- Type: {{ asset.assetType }}
- Name: {{ asset.name }}
- Description: {{ asset.description }}
- Profile: {{ asset.profile }}
- Base prompt: {{ asset.basePrompt }}
- Consistency prompt: {{ asset.consistencyPrompt }}
- Negative prompt: {{ asset.negativePrompt }}

Project style:
- Art style: {{ project.artStyle }}
- Director manual: {{ project.directorManual }}
- Visual manual: {{ project.visualManual }}
- Video ratio: {{ project.videoRatio }}

If the asset is a character, generate a character reference or costume baseline image.
If the asset is a scene, generate a reusable scene reference image.
If the asset is a prop, generate a reusable prop reference image.

Do not include text, watermarks, labels, storyboards, split panels, captions, or explanation text.
Keep the subject inspectable, centered, and consistent with the asset card.$prompt$)
,
    ('shot_video_prompt', 'Shot Video Prompt', 'Generate shot video using shot image and asset card context.', 'shot_video_prompt', 'video', 'video.create_task', $prompt$Create a {{ video.duration }}-second cinematic video from the reference shot image.

Shot:
- Visual: {{ shot.visual }}
- Camera: {{ shot.camera }}
- Motion: {{ shot.motion }}
- Mood: {{ shot.mood }}
- Video prompt: {{ shot.videoPrompt }}

Asset card context:
{{ assets.summary }}

Shot requirements:
{{ requirements.summary }}

Rules:
- Preserve the reference image composition and asset identity.
- Keep character appearance, hair, age range, and baseline costume stable unless the shot requirement explicitly changes them.
- Keep scene spatial structure, palette, and atmosphere stable.
- Keep prop form, scale, material, and position logic stable.
- Keep motion natural and cinematic.
- No subtitles, captions, title cards, watermarks, split-screen, collage, or extra characters.$prompt$)
)
INSERT INTO prompt_templates(
  organization_id, template_key, name, description, purpose, modality, task_type, scope, status, is_system
)
SELECT NULL, p.template_key, p.name, p.description, p.purpose, p.modality, p.task_type, 'system', 'active', true
FROM seed_prompts p
ON CONFLICT (template_key) WHERE organization_id IS NULL DO UPDATE SET
  name = EXCLUDED.name,
  description = EXCLUDED.description,
  purpose = EXCLUDED.purpose,
  modality = EXCLUDED.modality,
  task_type = EXCLUDED.task_type,
  scope = 'system',
  status = 'active',
  is_system = true,
  updated_at = now();

CREATE TEMP TABLE IF NOT EXISTS tmp_asset_card_seed_prompts(
  template_key TEXT PRIMARY KEY,
  content TEXT NOT NULL
);

TRUNCATE tmp_asset_card_seed_prompts;

INSERT INTO tmp_asset_card_seed_prompts(template_key, content)
VALUES
    ('asset_card_generation', $prompt$You are CineWeave's asset card designer.

Create a structured reusable asset card for the canonical asset.

Project:
{{ project }}

Asset:
{{ asset }}

Related script scenes:
{{ scenes }}

Return only valid JSON:
{
  "profile": {},
  "basePrompt": "prompt for generating the asset reference image",
  "consistencyPrompt": "stable identity and continuity prompt for later shots",
  "negativePrompt": "things that must not appear"
}

Rules:
- The profile object must contain stable visual fields appropriate for the asset type.
- Characters should include appearance, age range, body type, baseline costume, personality, visual keywords, and forbidden changes when known.
- Scenes should include space type, era, color palette, key elements, atmosphere, and forbidden changes when known.
- Props should include category, material, condition, size, key features, and forbidden changes when known.
- Keep names stable and avoid turning costumes, poses, or age states into character names.
- Do not include markdown fences or explanatory text.$prompt$),
    ('canonical_asset_image_prompt', $prompt$Create a clean reusable reference image for this canonical asset.

Asset:
- Type: {{ asset.assetType }}
- Name: {{ asset.name }}
- Description: {{ asset.description }}
- Profile: {{ asset.profile }}
- Base prompt: {{ asset.basePrompt }}
- Consistency prompt: {{ asset.consistencyPrompt }}
- Negative prompt: {{ asset.negativePrompt }}

Project style:
- Art style: {{ project.artStyle }}
- Director manual: {{ project.directorManual }}
- Visual manual: {{ project.visualManual }}
- Video ratio: {{ project.videoRatio }}

If the asset is a character, generate a character reference or costume baseline image.
If the asset is a scene, generate a reusable scene reference image.
If the asset is a prop, generate a reusable prop reference image.

Do not include text, watermarks, labels, storyboards, split panels, captions, or explanation text.
Keep the subject inspectable, centered, and consistent with the asset card.$prompt$)
,
    ('shot_video_prompt', $prompt$Create a {{ video.duration }}-second cinematic video from the reference shot image.

Shot:
- Visual: {{ shot.visual }}
- Camera: {{ shot.camera }}
- Motion: {{ shot.motion }}
- Mood: {{ shot.mood }}
- Video prompt: {{ shot.videoPrompt }}

Asset card context:
{{ assets.summary }}

Shot requirements:
{{ requirements.summary }}

Rules:
- Preserve the reference image composition and asset identity.
- Keep character appearance, hair, age range, and baseline costume stable unless the shot requirement explicitly changes them.
- Keep scene spatial structure, palette, and atmosphere stable.
- Keep prop form, scale, material, and position logic stable.
- Keep motion natural and cinematic.
- No subtitles, captions, title cards, watermarks, split-screen, collage, or extra characters.$prompt$)
;
UPDATE prompt_versions pv
SET status = 'archived'
FROM prompt_templates t
JOIN tmp_asset_card_seed_prompts p ON p.template_key = t.template_key
WHERE pv.template_id = t.id
  AND t.organization_id IS NULL
  AND t.template_key IN ('asset_card_generation', 'canonical_asset_image_prompt', 'shot_video_prompt')
  AND pv.status = 'active'
  AND pv.content_hash <> 'sha256:' || encode(digest(p.content, 'sha256'), 'hex');

INSERT INTO prompt_versions(
  prompt_template_id, template_id, version_no, version, status, title, content, content_format, variables_schema, metadata, content_hash, activated_at
)
SELECT t.id, t.id, COALESCE(MAX(v.version_no), 0) + 1, COALESCE(MAX(v.version), 0) + 1, 'active', 'Asset Cards v1',
       p.content, 'text', '{}'::jsonb, '{"seed":"asset_cards"}'::jsonb,
       'sha256:' || encode(digest(p.content, 'sha256'), 'hex'), now()
FROM tmp_asset_card_seed_prompts p
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
JOIN tmp_asset_card_seed_prompts p ON p.template_key = t.template_key
WHERE pv.template_id = t.id
  AND t.organization_id IS NULL
  AND t.template_key IN ('asset_card_generation', 'canonical_asset_image_prompt', 'shot_video_prompt')
  AND pv.content_hash = 'sha256:' || encode(digest(p.content, 'sha256'), 'hex')
  AND pv.id = (
    SELECT id
    FROM prompt_versions latest
    WHERE latest.template_id = t.id
      AND latest.content_hash = pv.content_hash
    ORDER BY latest.version_no DESC, latest.created_at DESC
    LIMIT 1
  );

INSERT INTO schema_migrations(version) VALUES ('000017_asset_cards')
ON CONFLICT (version) DO NOTHING;
