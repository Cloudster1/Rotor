-- +goose Up
-- The audit log filtered by what an entry is about (#36), such as everything that happened to one run.
CREATE INDEX audit_log_target_idx ON audit_log (target_type, target_id, created_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS audit_log_target_idx;
