DELETE FROM prompt_versions
WHERE metadata->>'seed' = 'review_fix_suggestions';

DELETE FROM prompt_templates
WHERE organization_id IS NULL
  AND template_key = 'review_fix_agent'
  AND is_system = true;

DROP INDEX IF EXISTS timeline_clips_project_stale_state_idx;
DROP INDEX IF EXISTS project_timelines_project_stale_state_idx;
DROP INDEX IF EXISTS idx_review_fixes_status;
DROP INDEX IF EXISTS idx_review_fixes_project;
DROP INDEX IF EXISTS idx_review_fixes_item;
DROP TRIGGER IF EXISTS review_fixes_set_updated_at ON review_fixes;
DROP TABLE IF EXISTS review_fixes;

ALTER TABLE timeline_clips
  DROP CONSTRAINT IF EXISTS timeline_clips_stale_state_check,
  DROP COLUMN IF EXISTS edited_at,
  DROP COLUMN IF EXISTS edited_by,
  DROP COLUMN IF EXISTS stale_state,
  DROP COLUMN IF EXISTS manual_override;

ALTER TABLE project_timelines
  DROP CONSTRAINT IF EXISTS project_timelines_stale_state_check,
  DROP COLUMN IF EXISTS edited_at,
  DROP COLUMN IF EXISTS edited_by,
  DROP COLUMN IF EXISTS stale_state,
  DROP COLUMN IF EXISTS manual_override;

ALTER TABLE review_items
  DROP CONSTRAINT IF EXISTS review_items_entity_type_check;

ALTER TABLE review_items
  ADD CONSTRAINT review_items_entity_type_check
  CHECK (entity_type IN ('script_scene', 'canonical_asset', 'storyboard_shot', 'shot_asset_requirement', 'timeline_clip', 'final_video_version', 'project'));

DELETE FROM schema_migrations WHERE version = '000023_review_fixes';
