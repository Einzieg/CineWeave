DROP TABLE IF EXISTS media_variants;
DROP TABLE IF EXISTS media_files;
DROP TABLE IF EXISTS asset_relations;
DROP TABLE IF EXISTS assets;
DROP TABLE IF EXISTS storyboard_shots;
DROP TABLE IF EXISTS storyboards;

ALTER TABLE scripts
  DROP CONSTRAINT IF EXISTS scripts_current_version_id_fk;

DROP TABLE IF EXISTS script_versions;
DROP TABLE IF EXISTS scripts;
DROP TABLE IF EXISTS novel_events;
DROP TABLE IF EXISTS novel_chapters;
DROP TABLE IF EXISTS novels;
