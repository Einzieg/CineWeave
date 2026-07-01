CREATE TABLE IF NOT EXISTS project_timelines (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
  title TEXT NOT NULL DEFAULT '默认时间线',
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'archived')),
  aspect_ratio TEXT NOT NULL DEFAULT '16:9',
  resolution TEXT NOT NULL DEFAULT '720p',
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  edited_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  edited_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS timeline_clips (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  timeline_id UUID NOT NULL REFERENCES project_timelines(id) ON DELETE CASCADE,
  storyboard_shot_id UUID REFERENCES storyboard_shots(id) ON DELETE SET NULL,
  video_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  video_media_file_id UUID REFERENCES media_files(id) ON DELETE SET NULL,
  clip_index INTEGER NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT true,
  source_storage_key TEXT,
  source_duration_seconds NUMERIC,
  trim_start_seconds NUMERIC NOT NULL DEFAULT 0,
  trim_end_seconds NUMERIC,
  target_duration_seconds NUMERIC,
  notes TEXT,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT timeline_clips_timeline_index_unique UNIQUE (timeline_id, clip_index) DEFERRABLE INITIALLY IMMEDIATE
);

CREATE TABLE IF NOT EXISTS final_video_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  timeline_id UUID NOT NULL REFERENCES project_timelines(id) ON DELETE CASCADE,
  workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
  version INTEGER NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('ready', 'active', 'archived', 'failed')),
  artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  media_file_id UUID REFERENCES media_files(id) ON DELETE SET NULL,
  storage_key TEXT,
  duration_seconds NUMERIC,
  resolution TEXT NOT NULL DEFAULT '720p',
  aspect_ratio TEXT NOT NULL DEFAULT '16:9',
  compose_settings JSONB NOT NULL DEFAULT '{}',
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT final_video_versions_project_version_unique UNIQUE (project_id, version) DEFERRABLE INITIALLY IMMEDIATE
);

ALTER TABLE projects
  ADD COLUMN IF NOT EXISTS active_final_video_version_id UUID;

ALTER TABLE projects
  DROP CONSTRAINT IF EXISTS projects_active_final_video_version_fk;

ALTER TABLE projects
  ADD CONSTRAINT projects_active_final_video_version_fk
  FOREIGN KEY (active_final_video_version_id) REFERENCES final_video_versions(id) ON DELETE SET NULL;

DROP TRIGGER IF EXISTS project_timelines_set_updated_at ON project_timelines;
CREATE TRIGGER project_timelines_set_updated_at
BEFORE UPDATE ON project_timelines
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS timeline_clips_set_updated_at ON timeline_clips;
CREATE TRIGGER timeline_clips_set_updated_at
BEFORE UPDATE ON timeline_clips
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS project_timelines_project_created_idx
  ON project_timelines(project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS project_timelines_project_status_idx
  ON project_timelines(project_id, status);

CREATE INDEX IF NOT EXISTS timeline_clips_timeline_order_idx
  ON timeline_clips(timeline_id, clip_index);

CREATE INDEX IF NOT EXISTS timeline_clips_project_shot_idx
  ON timeline_clips(project_id, storyboard_shot_id);

CREATE INDEX IF NOT EXISTS final_video_versions_project_created_idx
  ON final_video_versions(project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS final_video_versions_project_status_idx
  ON final_video_versions(project_id, status);

INSERT INTO schema_migrations(version) VALUES ('000020_timeline_final_video')
ON CONFLICT (version) DO NOTHING;
