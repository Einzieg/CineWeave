-- +goose Up

SET search_path TO public;

ALTER TABLE video_prompt_plan_dialogue_cues
    ADD COLUMN timing_unit_id TEXT,
    ADD COLUMN continues_from_previous BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN continues_to_next BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE video_prompt_plan_dialogue_cues
    DISABLE TRIGGER video_prompt_plan_dialogue_cues_immutable;

WITH authoritative_cues AS (
    SELECT plan.id AS video_prompt_plan_id,
           cue.ordinality - 1 AS ordinal,
           NULLIF(btrim(cue.value->>'timingUnitId'), '') AS timing_unit_id,
           CASE lower(btrim(COALESCE(cue.value->>'kind', 'dialogue')))
               WHEN 'voiceover' THEN 'voiceover'
               WHEN 'narration' THEN 'narration'
               WHEN 'system' THEN 'system'
               ELSE 'dialogue'
           END AS kind,
           COALESCE((cue.value->>'continuesFromPrevious')::boolean, false) AS continues_from_previous,
           COALESCE((cue.value->>'continuesToNext')::boolean, false) AS continues_to_next
    FROM video_prompt_plans plan
    CROSS JOIN LATERAL jsonb_array_elements(plan.dialogue_cues) WITH ORDINALITY AS cue(value, ordinality)
)
UPDATE video_prompt_plan_dialogue_cues stored
SET timing_unit_id = authoritative.timing_unit_id,
    kind = authoritative.kind,
    continues_from_previous = authoritative.continues_from_previous,
    continues_to_next = authoritative.continues_to_next,
    speaker = CASE
        WHEN authoritative.kind = 'system' THEN '系统音频'
        ELSE stored.speaker
    END
FROM authoritative_cues authoritative
WHERE stored.video_prompt_plan_id = authoritative.video_prompt_plan_id
  AND stored.ordinal = authoritative.ordinal;

ALTER TABLE video_prompt_plan_dialogue_cues
    ENABLE TRIGGER video_prompt_plan_dialogue_cues_immutable;

WITH canonical_dialogue AS (
    SELECT segment.id,
           jsonb_agg(
               jsonb_build_object(
                   'timingUnitId', COALESCE(NULLIF(cue.value->>'timingUnitId', ''), NULLIF(authoritative.value->>'timingUnitId', ''), ''),
                   'speaker', CASE
                       WHEN COALESCE(NULLIF(authoritative.value->>'kind', ''), NULLIF(cue.value->>'kind', ''), 'dialogue') = 'system'
                           THEN '系统音频'
                       ELSE COALESCE(NULLIF(cue.value->>'speaker', ''), NULLIF(authoritative.value->>'speaker', ''), '旁白')
                   END,
                   'text', COALESCE(cue.value->>'text', ''),
                   'delivery', COALESCE(NULLIF(cue.value->>'delivery', ''), NULLIF(authoritative.value->>'delivery', ''), ''),
                   'kind', COALESCE(NULLIF(authoritative.value->>'kind', ''), NULLIF(cue.value->>'kind', ''), 'dialogue'),
                   'startTick', COALESCE(NULLIF(cue.value->>'startTick', '')::bigint, NULLIF(cue.value->>'spanStartTick', '')::bigint, 0),
                   'endTick', COALESCE(NULLIF(cue.value->>'endTick', '')::bigint, NULLIF(cue.value->>'spanEndTick', '')::bigint, 0),
                   'continuesFromPrevious', COALESCE((cue.value->>'continuesFromPrevious')::boolean, false),
                   'continuesToNext', COALESCE((cue.value->>'continuesToNext')::boolean, false)
               )
               ORDER BY cue.ordinality
           ) AS dialogue
    FROM video_render_segments segment
    JOIN video_render_plans render_plan ON render_plan.id = segment.video_render_plan_id
    JOIN video_prompt_plans prompt_plan ON prompt_plan.id = render_plan.video_prompt_plan_id
    CROSS JOIN LATERAL jsonb_array_elements(
        CASE WHEN jsonb_typeof(segment.dialogue) = 'array' THEN segment.dialogue ELSE '[]'::jsonb END
    ) WITH ORDINALITY AS cue(value, ordinality)
    LEFT JOIN LATERAL (
        SELECT candidate.value
        FROM jsonb_array_elements(
            CASE WHEN jsonb_typeof(prompt_plan.dialogue_cues) = 'array' THEN prompt_plan.dialogue_cues ELSE '[]'::jsonb END
        ) AS candidate(value)
        WHERE btrim(COALESCE(candidate.value->>'text', '')) <> ''
          AND (
              btrim(candidate.value->>'text') = btrim(cue.value->>'text')
              OR position(btrim(cue.value->>'text') IN btrim(candidate.value->>'text')) > 0
              OR position(btrim(candidate.value->>'text') IN btrim(cue.value->>'text')) > 0
          )
        ORDER BY (btrim(candidate.value->>'text') = btrim(cue.value->>'text')) DESC,
                 length(candidate.value->>'text') ASC
        LIMIT 1
    ) authoritative ON true
    GROUP BY segment.id
)
UPDATE video_render_segments segment
SET dialogue = canonical.dialogue,
    updated_at = now()
FROM canonical_dialogue canonical
WHERE segment.id = canonical.id;

-- +goose Down

SET search_path TO public;

ALTER TABLE video_prompt_plan_dialogue_cues
    DROP COLUMN IF EXISTS continues_to_next,
    DROP COLUMN IF EXISTS continues_from_previous,
    DROP COLUMN IF EXISTS timing_unit_id;
