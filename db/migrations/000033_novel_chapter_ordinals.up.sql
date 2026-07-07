ALTER TABLE novel_chapters
  ADD COLUMN IF NOT EXISTS volume_index INT,
  ADD COLUMN IF NOT EXISTS section_index INT;

CREATE OR REPLACE FUNCTION cineweave_parse_ordinal(input TEXT)
RETURNS INT AS $$
DECLARE
  text_value TEXT := trim(input);
  ch TEXT;
  idx INT;
  digit INT;
  unit_value INT;
  total INT := 0;
  section INT := 0;
  number_value INT := 0;
BEGIN
  IF text_value IS NULL OR text_value = '' THEN
    RETURN NULL;
  END IF;

  text_value := translate(text_value, '０１２３４５６７８９', '0123456789');
  IF text_value ~ '^[0-9]+$' THEN
    RETURN text_value::INT;
  END IF;

  FOR idx IN 1..char_length(text_value) LOOP
    ch := substr(text_value, idx, 1);
    digit := CASE ch
      WHEN '零' THEN 0 WHEN '〇' THEN 0
      WHEN '一' THEN 1 WHEN '壹' THEN 1
      WHEN '二' THEN 2 WHEN '两' THEN 2 WHEN '贰' THEN 2
      WHEN '三' THEN 3 WHEN '叁' THEN 3
      WHEN '四' THEN 4 WHEN '肆' THEN 4
      WHEN '五' THEN 5 WHEN '伍' THEN 5
      WHEN '六' THEN 6 WHEN '陆' THEN 6
      WHEN '七' THEN 7 WHEN '柒' THEN 7
      WHEN '八' THEN 8 WHEN '捌' THEN 8
      WHEN '九' THEN 9 WHEN '玖' THEN 9
      ELSE NULL
    END;
    IF digit IS NOT NULL THEN
      number_value := digit;
      CONTINUE;
    END IF;

    unit_value := CASE ch
      WHEN '十' THEN 10 WHEN '拾' THEN 10
      WHEN '百' THEN 100 WHEN '佰' THEN 100
      WHEN '千' THEN 1000 WHEN '仟' THEN 1000
      WHEN '万' THEN 10000
      WHEN '亿' THEN 100000000
      ELSE NULL
    END;
    IF unit_value IS NULL THEN
      RETURN NULL;
    END IF;

    IF unit_value = 10000 OR unit_value = 100000000 THEN
      section := section + number_value;
      IF section = 0 THEN
        section := 1;
      END IF;
      total := total + section * unit_value;
      section := 0;
      number_value := 0;
      CONTINUE;
    END IF;

    IF number_value = 0 THEN
      number_value := 1;
    END IF;
    section := section + number_value * unit_value;
    number_value := 0;
  END LOOP;

  IF total + section + number_value <= 0 THEN
    RETURN NULL;
  END IF;
  RETURN total + section + number_value;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

WITH parsed AS (
  SELECT id,
         cineweave_parse_ordinal((regexp_match(COALESCE(volume_title, ''), '第?[[:space:]]*([0-9０-９一二两三四五六七八九十百千万亿〇零壹贰叁肆伍陆柒捌玖拾佰仟]+)[[:space:]]*(卷|部|篇)'))[1]) AS parsed_volume_index,
         cineweave_parse_ordinal((regexp_match(COALESCE(chapter_title, ''), '第?[[:space:]]*([0-9０-９一二两三四五六七八九十百千万亿〇零壹贰叁肆伍陆柒捌玖拾佰仟]+)[[:space:]]*(章|节|回|幕|集|话)'))[1]) AS parsed_section_index
  FROM novel_chapters
  WHERE source_id IS NOT NULL
)
UPDATE novel_chapters c
SET volume_index = COALESCE(c.volume_index, parsed.parsed_volume_index),
    section_index = COALESCE(c.section_index, parsed.parsed_section_index, c.chapter_index)
FROM parsed
WHERE c.id = parsed.id;

DROP FUNCTION IF EXISTS cineweave_parse_ordinal(TEXT);

CREATE INDEX IF NOT EXISTS novel_chapters_source_volume_section_idx
  ON novel_chapters(source_id, volume_index, section_index, chapter_index)
  WHERE source_id IS NOT NULL;

INSERT INTO schema_migrations(version)
VALUES ('000033_novel_chapter_ordinals')
ON CONFLICT (version) DO NOTHING;
