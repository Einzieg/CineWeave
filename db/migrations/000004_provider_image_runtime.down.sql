ALTER TABLE media_files
  DROP COLUMN IF EXISTS metadata;

ALTER TABLE media_files
  DROP COLUMN IF EXISTS created_by;

ALTER TABLE artifacts
  ALTER COLUMN project_id SET NOT NULL;

DELETE FROM schema_migrations WHERE version = '000004_provider_image_runtime';
