CREATE TABLE IF NOT EXISTS project_exports (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  export_type TEXT NOT NULL CHECK (export_type IN ('final_video', 'documents', 'asset_package', 'project_archive')),
  status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
  title TEXT NOT NULL,
  format TEXT NOT NULL CHECK (format IN ('mp4', 'json', 'markdown', 'zip')),
  workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
  artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  media_file_id UUID REFERENCES media_files(id) ON DELETE SET NULL,
  storage_key TEXT,
  byte_size BIGINT,
  content_hash TEXT,
  request JSONB NOT NULL DEFAULT '{}',
  output JSONB NOT NULL DEFAULT '{}',
  error_code TEXT,
  error_message TEXT,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_project_exports_project
  ON project_exports(project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_project_exports_status
  ON project_exports(project_id, status);

INSERT INTO schema_migrations(version) VALUES ('000021_project_exports')
ON CONFLICT (version) DO NOTHING;
