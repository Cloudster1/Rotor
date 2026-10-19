-- +goose Up
-- An approval's message, preview and values are sealed with its run's data key, bound to its interrupt, like the
-- rest of the run's data (#91). Nothing has been released, so runs kept with approvals in clear are deleted rather
-- than converted, as 00800 did; the audit log keeps their record.
TRUNCATE run CASCADE;
UPDATE trigger SET last_run_id = NULL WHERE last_run_id IS NOT NULL;
UPDATE trigger_fire SET run_id = NULL, inputs = '{}'::jsonb WHERE done_at IS NOT NULL;

ALTER TABLE approval DROP COLUMN message, DROP COLUMN preview, DROP COLUMN form_values, DROP COLUMN approved_values,
    ADD COLUMN sealed_message bytea NOT NULL,
    ADD COLUMN sealed_preview bytea NOT NULL,
    ADD COLUMN sealed_form_values bytea NOT NULL,
    ADD COLUMN sealed_approved_values bytea;

-- +goose Down
TRUNCATE run CASCADE;
ALTER TABLE approval DROP COLUMN sealed_message, DROP COLUMN sealed_preview, DROP COLUMN sealed_form_values,
    DROP COLUMN sealed_approved_values,
    ADD COLUMN message text NOT NULL,
    ADD COLUMN preview jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN form_values jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN approved_values jsonb;
