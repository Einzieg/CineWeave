DELETE FROM prompt_versions
WHERE metadata->>'seed' = 'asset_cards';

UPDATE prompt_versions latest
SET status = 'active', activated_at = COALESCE(activated_at, now())
WHERE latest.id IN (
  SELECT DISTINCT ON (pv.template_id) pv.id
  FROM prompt_versions pv
  JOIN prompt_templates t ON t.id = pv.template_id
  WHERE t.organization_id IS NULL
    AND t.template_key IN ('asset_card_generation', 'canonical_asset_image_prompt', 'shot_video_prompt')
  ORDER BY pv.template_id, pv.version_no DESC, pv.created_at DESC
);

DELETE FROM prompt_templates
WHERE organization_id IS NULL
  AND template_key = 'asset_card_generation'
  AND NOT EXISTS (
    SELECT 1
    FROM prompt_versions
    WHERE template_id = prompt_templates.id
  );

DROP INDEX IF EXISTS idx_asset_references_one_primary;
DROP INDEX IF EXISTS idx_asset_references_project;
DROP INDEX IF EXISTS idx_asset_references_asset;

DROP TRIGGER IF EXISTS asset_references_set_updated_at ON asset_references;
DROP TABLE IF EXISTS asset_references;

ALTER TABLE canonical_assets
  DROP COLUMN IF EXISTS lock_reference,
  DROP COLUMN IF EXISTS primary_reference_storage_key,
  DROP COLUMN IF EXISTS primary_reference_media_file_id,
  DROP COLUMN IF EXISTS primary_reference_artifact_id,
  DROP COLUMN IF EXISTS consistency_prompt,
  DROP COLUMN IF EXISTS negative_prompt,
  DROP COLUMN IF EXISTS profile;

DELETE FROM schema_migrations WHERE version = '000017_asset_cards';
