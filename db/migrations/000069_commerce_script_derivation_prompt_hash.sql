-- +goose Up
ALTER TABLE commerce_script_derivation_attempt_calls
    DROP CONSTRAINT commerce_script_derivation_attempt_calls_hash_check;

ALTER TABLE commerce_script_derivation_attempt_calls
    ADD CONSTRAINT commerce_script_derivation_attempt_calls_hash_check CHECK (
        prompt_hash ~ '^(sha256:)?[0-9a-f]{64}$'
        AND (output_content_hash IS NULL OR output_content_hash ~ '^[0-9a-f]{64}$')
    );

-- +goose Down
ALTER TABLE commerce_script_derivation_attempt_calls
    DROP CONSTRAINT commerce_script_derivation_attempt_calls_hash_check;

ALTER TABLE commerce_script_derivation_attempt_calls
    ADD CONSTRAINT commerce_script_derivation_attempt_calls_hash_check CHECK (
        prompt_hash ~ '^[0-9a-f]{64}$'
        AND (output_content_hash IS NULL OR output_content_hash ~ '^[0-9a-f]{64}$')
    );
