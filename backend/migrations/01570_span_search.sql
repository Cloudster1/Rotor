-- +goose Up
-- Searching spans across the traces a person reads (#261). docs/api-contract-m18.md says why each index is here: a
-- search always names a time window and reads newest first, so each index ends in the search's own order, and a
-- page walks one backwards until it has enough spans the caller reads. Only leakproof comparisons (timestamps,
-- text equality) may run ahead of row level security, so the name, model and attribute filters get no index.
CREATE INDEX trace_span_started ON trace_span (started_at, trace, span_id);
CREATE INDEX trace_span_kind_started ON trace_span (kind, started_at, trace, span_id);
-- Failed spans are the few a person most often searches for.
CREATE INDEX trace_span_failed_started ON trace_span (started_at, trace, span_id) WHERE status = 'error';

-- +goose Down
DROP INDEX IF EXISTS trace_span_failed_started;
DROP INDEX IF EXISTS trace_span_kind_started;
DROP INDEX IF EXISTS trace_span_started;
