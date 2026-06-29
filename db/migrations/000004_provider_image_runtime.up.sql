ALTER TABLE artifacts
  ALTER COLUMN project_id DROP NOT NULL;

ALTER TABLE media_files
  ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE media_files
  ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';

UPDATE provider_connectors
SET manifest = jsonb_set(
  COALESCE(manifest, '{}'::jsonb),
  '{endpoints,imagesGenerations}',
  '{"method":"POST","path":"/images/generations"}'::jsonb,
  true
)
WHERE connector_key = 'openai_compatible';

INSERT INTO schema_migrations(version) VALUES ('000004_provider_image_runtime')
ON CONFLICT (version) DO NOTHING;
