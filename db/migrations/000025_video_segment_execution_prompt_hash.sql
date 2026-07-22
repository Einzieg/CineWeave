-- +goose Up

SET search_path TO public;

ALTER TABLE video_render_segments
    ADD COLUMN execution_prompt_hash TEXT;

UPDATE video_render_segments
SET execution_prompt_hash = 'sha256:' || encode(digest(convert_to(prompt, 'UTF8'), 'sha256'), 'hex')
WHERE prompt IS NOT NULL AND btrim(prompt) <> '';

ALTER TABLE video_render_segments
    ADD CONSTRAINT video_render_segments_execution_prompt_hash_check CHECK (
        execution_prompt_hash IS NULL OR execution_prompt_hash ~ '^sha256:[0-9a-f]{64}$'
    ),
    ADD CONSTRAINT video_render_segments_execution_prompt_pair_check CHECK (
        ((prompt IS NULL OR btrim(prompt) = '') AND execution_prompt_hash IS NULL)
        OR (prompt IS NOT NULL AND btrim(prompt) <> '' AND execution_prompt_hash IS NOT NULL)
    );

CREATE INDEX video_render_segments_execution_prompt_idx
    ON video_render_segments(video_render_plan_id, segment_index, execution_prompt_hash)
    WHERE execution_prompt_hash IS NOT NULL;

-- +goose Down

SET search_path TO public;

DROP INDEX IF EXISTS video_render_segments_execution_prompt_idx;

ALTER TABLE video_render_segments
    DROP CONSTRAINT IF EXISTS video_render_segments_execution_prompt_pair_check,
    DROP CONSTRAINT IF EXISTS video_render_segments_execution_prompt_hash_check,
    DROP COLUMN IF EXISTS execution_prompt_hash;
