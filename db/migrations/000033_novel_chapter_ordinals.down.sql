DROP INDEX IF EXISTS novel_chapters_source_volume_section_idx;

ALTER TABLE novel_chapters
  DROP COLUMN IF EXISTS section_index,
  DROP COLUMN IF EXISTS volume_index;

DELETE FROM schema_migrations
WHERE version = '000033_novel_chapter_ordinals';
