-- +goose Up

SET search_path TO public;

CREATE TABLE auth_security_failures (
    scope TEXT NOT NULL,
    subject_hash TEXT NOT NULL,
    failure_count INTEGER NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    window_started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    blocked_until TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (scope, subject_hash),
    CHECK (length(subject_hash) = 64)
);

CREATE INDEX auth_security_failures_updated_at_idx
    ON auth_security_failures(updated_at);

-- +goose Down

SET search_path TO public;

DROP TABLE IF EXISTS auth_security_failures;
