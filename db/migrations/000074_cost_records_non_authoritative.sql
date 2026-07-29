-- +goose Up
SET search_path TO public;

ALTER TABLE cost_records
    ADD COLUMN billing_authoritative BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT cost_records_never_billing_authoritative_check CHECK (
        billing_authoritative = false
    );

COMMENT ON TABLE cost_records IS
    'Technical Provider usage estimates and provenance only. New API is the sole commercial balance, transaction, invoice, and spend authority.';
COMMENT ON COLUMN cost_records.amount IS
    'Non-authoritative technical estimate; never use for wallet balance, invoice amount, or monetary spend gates.';
COMMENT ON COLUMN cost_records.billing_authoritative IS
    'Always false. Authoritative commercial transactions live in the private Billing Bridge projection.';

-- +goose Down
SET search_path TO public;

COMMENT ON TABLE cost_records IS NULL;
COMMENT ON COLUMN cost_records.amount IS NULL;

ALTER TABLE cost_records
    DROP CONSTRAINT cost_records_never_billing_authoritative_check,
    DROP COLUMN billing_authoritative;
