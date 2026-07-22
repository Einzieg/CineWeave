-- +goose Up

SET search_path TO public;

ALTER TABLE video_prompt_plan_dialogue_cues
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'dialogue';

ALTER TABLE video_prompt_plan_dialogue_cues
    DISABLE TRIGGER video_prompt_plan_dialogue_cues_immutable;

WITH authoritative_cues AS (
    SELECT plan.id AS video_prompt_plan_id,
           cue.ordinality - 1 AS ordinal,
           CASE lower(btrim(COALESCE(cue.value->>'kind', 'dialogue')))
               WHEN 'voiceover' THEN 'voiceover'
               WHEN 'narration' THEN 'narration'
               WHEN 'system' THEN 'system'
               ELSE 'dialogue'
           END AS kind
    FROM video_prompt_plans plan
    CROSS JOIN LATERAL jsonb_array_elements(plan.dialogue_cues) WITH ORDINALITY AS cue(value, ordinality)
)
UPDATE video_prompt_plan_dialogue_cues stored
SET kind = authoritative.kind,
    speaker = CASE
        WHEN authoritative.kind = 'system' AND btrim(stored.speaker) = '旁白' THEN '系统音频'
        ELSE stored.speaker
    END
FROM authoritative_cues authoritative
WHERE stored.video_prompt_plan_id = authoritative.video_prompt_plan_id
  AND stored.ordinal = authoritative.ordinal;

ALTER TABLE video_prompt_plan_dialogue_cues
    ENABLE TRIGGER video_prompt_plan_dialogue_cues_immutable;

ALTER TABLE video_prompt_plan_dialogue_cues
    ADD CONSTRAINT video_prompt_plan_dialogue_cues_kind_check CHECK (
        kind IN ('dialogue', 'voiceover', 'narration', 'system')
    );

CREATE INDEX video_prompt_plan_dialogue_cues_kind_idx
    ON video_prompt_plan_dialogue_cues(video_prompt_plan_id, ordinal, kind);

-- +goose Down

SET search_path TO public;

DROP INDEX IF EXISTS video_prompt_plan_dialogue_cues_kind_idx;

ALTER TABLE video_prompt_plan_dialogue_cues
    DROP CONSTRAINT IF EXISTS video_prompt_plan_dialogue_cues_kind_check,
    DROP COLUMN IF EXISTS kind;
