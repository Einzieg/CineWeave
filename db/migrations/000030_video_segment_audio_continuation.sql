-- +goose Up

SET search_path TO public;

WITH repaired_dialogue AS (
    SELECT segment.id,
           jsonb_agg(
               CASE WHEN authoritative.value IS NULL THEN cue.value
                    ELSE cue.value || jsonb_build_object(
                        'continuesFromPrevious', COALESCE((authoritative.value->>'continuesFromPrevious')::boolean, false),
                        'continuesToNext', COALESCE((authoritative.value->>'continuesToNext')::boolean, false)
                    )
               END
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
        WHERE btrim(COALESCE(candidate.value->>'text', '')) = btrim(COALESCE(cue.value->>'text', ''))
        LIMIT 1
    ) authoritative ON true
    GROUP BY segment.id
)
UPDATE video_render_segments segment
SET dialogue = repaired.dialogue,
    updated_at = now()
FROM repaired_dialogue repaired
WHERE segment.id = repaired.id
  AND segment.dialogue IS DISTINCT FROM repaired.dialogue;

-- +goose Down

SELECT 1;
