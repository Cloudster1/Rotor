-- +goose Up
-- A run's W3C trace context, made when it is started, so the worker's and the runner's spans join its trace (#37).
ALTER TABLE run ADD COLUMN traceparent text NOT NULL DEFAULT ''
    CHECK (traceparent = '' OR traceparent ~ '^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$');

-- +goose Down
ALTER TABLE run DROP COLUMN IF EXISTS traceparent;
