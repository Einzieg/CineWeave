-- +goose Up

SET search_path TO public;

ALTER TABLE commerce_setup_runs
    DROP CONSTRAINT commerce_setup_runs_setup_session_id_key,
    ADD COLUMN attempt_no INTEGER NOT NULL DEFAULT 1,
    ADD CONSTRAINT commerce_setup_runs_attempt_no_check CHECK (attempt_no > 0);

CREATE UNIQUE INDEX commerce_setup_runs_session_attempt_unique
    ON commerce_setup_runs(setup_session_id, attempt_no);

-- +goose Down

SET search_path TO public;

DELETE FROM commerce_setup_runs older
USING commerce_setup_runs newer
WHERE older.setup_session_id = newer.setup_session_id
  AND older.attempt_no < newer.attempt_no;

DROP INDEX IF EXISTS commerce_setup_runs_session_attempt_unique;

ALTER TABLE commerce_setup_runs
    DROP CONSTRAINT commerce_setup_runs_attempt_no_check,
    DROP COLUMN attempt_no,
    ADD CONSTRAINT commerce_setup_runs_setup_session_id_key UNIQUE(setup_session_id);
