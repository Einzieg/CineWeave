CREATE TABLE IF NOT EXISTS novels (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  source_type TEXT,
  raw_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  clean_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS novel_chapters (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  novel_id UUID NOT NULL REFERENCES novels(id) ON DELETE CASCADE,
  chapter_index INT NOT NULL,
  title TEXT,
  content_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(novel_id, chapter_index)
);

CREATE TABLE IF NOT EXISTS novel_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  novel_id UUID REFERENCES novels(id) ON DELETE CASCADE,
  chapter_id UUID REFERENCES novel_chapters(id) ON DELETE CASCADE,
  event_index INT NOT NULL,
  event_type TEXT,
  summary TEXT NOT NULL,
  characters JSONB NOT NULL DEFAULT '[]',
  scenes JSONB NOT NULL DEFAULT '[]',
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS scripts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  current_version_id UUID,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS script_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  script_id UUID NOT NULL REFERENCES scripts(id) ON DELETE CASCADE,
  version_no INT NOT NULL,
  content_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(script_id, version_no)
);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'scripts_current_version_id_fk'
  ) THEN
    ALTER TABLE scripts
      ADD CONSTRAINT scripts_current_version_id_fk
      FOREIGN KEY (current_version_id) REFERENCES script_versions(id) ON DELETE SET NULL;
  END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS storyboards (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  script_id UUID REFERENCES scripts(id) ON DELETE SET NULL,
  title TEXT NOT NULL,
  current_version_no INT NOT NULL DEFAULT 1,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS storyboard_shots (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  storyboard_id UUID NOT NULL REFERENCES storyboards(id) ON DELETE CASCADE,
  script_version_id UUID REFERENCES script_versions(id) ON DELETE SET NULL,
  shot_index INT NOT NULL,
  duration_seconds NUMERIC,
  shot_size TEXT,
  camera_move TEXT,
  action TEXT,
  dialogue TEXT,
  asset_bindings JSONB NOT NULL DEFAULT '[]',
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(storyboard_id, shot_index)
);

CREATE TABLE IF NOT EXISTS assets (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  asset_type TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  current_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS asset_relations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  source_asset_id UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  target_asset_id UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  relation_type TEXT NOT NULL,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS media_files (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
  artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  storage_key TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  byte_size BIGINT,
  width INT,
  height INT,
  duration_seconds NUMERIC,
  checksum TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS media_variants (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  media_file_id UUID NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
  variant_type TEXT NOT NULL,
  storage_key TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS novels_set_updated_at ON novels;
CREATE TRIGGER novels_set_updated_at
BEFORE UPDATE ON novels
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS scripts_set_updated_at ON scripts;
CREATE TRIGGER scripts_set_updated_at
BEFORE UPDATE ON scripts
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS storyboards_set_updated_at ON storyboards;
CREATE TRIGGER storyboards_set_updated_at
BEFORE UPDATE ON storyboards
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS storyboard_shots_set_updated_at ON storyboard_shots;
CREATE TRIGGER storyboard_shots_set_updated_at
BEFORE UPDATE ON storyboard_shots
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS assets_set_updated_at ON assets;
CREATE TRIGGER assets_set_updated_at
BEFORE UPDATE ON assets
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS novels_project_idx ON novels(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS novel_events_project_idx ON novel_events(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS scripts_project_idx ON scripts(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS storyboards_project_idx ON storyboards(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS storyboard_shots_storyboard_idx ON storyboard_shots(storyboard_id, shot_index);
CREATE INDEX IF NOT EXISTS assets_project_type_idx ON assets(project_id, asset_type, created_at DESC);
CREATE INDEX IF NOT EXISTS media_files_project_idx ON media_files(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS media_files_artifact_idx ON media_files(artifact_id);
