UPDATE project_sources
SET source_type = 'script'
WHERE source_type = 'brief';

ALTER TABLE project_sources
  DROP CONSTRAINT IF EXISTS project_sources_source_type_check;

ALTER TABLE project_sources
  ADD CONSTRAINT project_sources_source_type_check
  CHECK (source_type IN ('novel', 'script'));
