-- +goose Up
-- Parallel branches (#135) wait at several approvals at once: every one is in the inbox, and the run goes on once
-- each is decided. Approvals one Done opens share requested_at, the transaction's now(), which ties them together.
DROP INDEX approval_one_pending_idx;
CREATE INDEX approval_run_pending_idx ON approval (run_id) WHERE status = 'pending';

-- +goose Down
DROP INDEX IF EXISTS approval_run_pending_idx;
CREATE UNIQUE INDEX approval_one_pending_idx ON approval (run_id) WHERE status = 'pending';
