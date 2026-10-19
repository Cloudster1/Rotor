-- +goose Up
-- A run's data at rest is sealed with a data key of the run's own, which OpenBao transit makes and wraps (#41):
-- the wrapped key is kept with the run, and what a run read from the tools is ciphertext in every table. Nothing has
-- been released, so runs kept in plaintext are deleted rather than converted; the audit log keeps their record.
TRUNCATE run CASCADE;
UPDATE trigger SET last_run_id = NULL WHERE last_run_id IS NOT NULL;
UPDATE trigger_fire SET run_id = NULL, inputs = '{}'::jsonb WHERE done_at IS NOT NULL;

DROP POLICY run_own_start ON run;
ALTER TABLE run DROP COLUMN inputs, DROP COLUMN output, DROP COLUMN resume,
    ADD COLUMN data_key text NOT NULL CHECK (data_key LIKE 'vault:v%:%'),
    ADD COLUMN sealed_inputs bytea NOT NULL,
    ADD COLUMN sealed_output bytea;
CREATE POLICY run_own_start ON run FOR INSERT TO rotor_app
    WITH CHECK (user_id = current_actor_id() AND status = 'queued' AND attempts = 0 AND runner_seq = 0
        AND sandbox_id IS NULL AND claimed_by IS NULL AND sealed_output IS NULL
        AND EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow_id AND w.archived_at IS NULL)
        AND (trigger_id IS NULL OR EXISTS (SELECT 1 FROM trigger t WHERE t.id = trigger_id
            AND t.owner_id = current_actor_id() AND t.enabled AND t.workflow_id = run.workflow_id)));

ALTER TABLE run_event DROP COLUMN payload, ADD COLUMN sealed_payload bytea NOT NULL;
ALTER TABLE run_control DROP COLUMN message, ADD COLUMN sealed_message bytea NOT NULL;
ALTER TABLE run_checkpoint DROP COLUMN checkpoint, DROP COLUMN metadata,
    ADD COLUMN sealed_checkpoint bytea NOT NULL, ADD COLUMN sealed_metadata bytea NOT NULL;
ALTER TABLE run_checkpoint_write DROP COLUMN value, ADD COLUMN sealed_value bytea NOT NULL;

-- +goose Down
TRUNCATE run CASCADE;
ALTER TABLE run_checkpoint_write DROP COLUMN sealed_value, ADD COLUMN value text NOT NULL;
ALTER TABLE run_checkpoint DROP COLUMN sealed_checkpoint, DROP COLUMN sealed_metadata,
    ADD COLUMN checkpoint text NOT NULL, ADD COLUMN metadata text NOT NULL;
ALTER TABLE run_control DROP COLUMN sealed_message, ADD COLUMN message bytea NOT NULL;
ALTER TABLE run_event DROP COLUMN sealed_payload, ADD COLUMN payload jsonb NOT NULL DEFAULT '{}'::jsonb;
DROP POLICY run_own_start ON run;
ALTER TABLE run DROP COLUMN data_key, DROP COLUMN sealed_inputs, DROP COLUMN sealed_output,
    ADD COLUMN inputs jsonb NOT NULL DEFAULT '{}'::jsonb, ADD COLUMN output jsonb, ADD COLUMN resume jsonb;
CREATE POLICY run_own_start ON run FOR INSERT TO rotor_app
    WITH CHECK (user_id = current_actor_id() AND status = 'queued' AND attempts = 0 AND runner_seq = 0
        AND sandbox_id IS NULL AND claimed_by IS NULL AND resume IS NULL AND output IS NULL
        AND EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow_id AND w.archived_at IS NULL)
        AND (trigger_id IS NULL OR EXISTS (SELECT 1 FROM trigger t WHERE t.id = trigger_id
            AND t.owner_id = current_actor_id() AND t.enabled AND t.workflow_id = run.workflow_id)));
