-- +goose Up
-- A run's LangGraph checkpoints, kept by the control plane so a run outlives its sandbox (#19). Every value is
-- OpenBao transit ciphertext; the type tags LangGraph's serializer writes beside them are not secret.
CREATE TABLE run_checkpoint (
    run_id               uuid NOT NULL REFERENCES run(id) ON DELETE CASCADE,
    checkpoint_ns        text NOT NULL DEFAULT '',
    checkpoint_id        text NOT NULL,
    parent_checkpoint_id text NOT NULL DEFAULT '',
    checkpoint_type      text NOT NULL,
    checkpoint           text NOT NULL,
    metadata_type        text NOT NULL,
    metadata             text NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, checkpoint_ns, checkpoint_id)
);

-- What a task wrote after a checkpoint, so a resumed run does not do it again.
CREATE TABLE run_checkpoint_write (
    run_id        uuid NOT NULL,
    checkpoint_ns text NOT NULL DEFAULT '',
    checkpoint_id text NOT NULL,
    task_id       text NOT NULL,
    idx           integer NOT NULL,
    channel       text NOT NULL,
    value_type    text NOT NULL,
    value         text NOT NULL,
    task_path     text NOT NULL DEFAULT '',
    PRIMARY KEY (run_id, checkpoint_ns, checkpoint_id, task_id, idx),
    FOREIGN KEY (run_id, checkpoint_ns, checkpoint_id) REFERENCES run_checkpoint (run_id, checkpoint_ns, checkpoint_id) ON DELETE CASCADE
);

ALTER TABLE run_checkpoint ENABLE ROW LEVEL SECURITY;
ALTER TABLE run_checkpoint FORCE  ROW LEVEL SECURITY;
ALTER TABLE run_checkpoint_write ENABLE ROW LEVEL SECURITY;
ALTER TABLE run_checkpoint_write FORCE  ROW LEVEL SECURITY;
CREATE POLICY run_checkpoint_admin_bypass ON run_checkpoint TO rotor_admin USING (true) WITH CHECK (true);
CREATE POLICY run_checkpoint_write_admin_bypass ON run_checkpoint_write TO rotor_admin USING (true) WITH CHECK (true);
REVOKE ALL ON run_checkpoint, run_checkpoint_write FROM rotor_app;

-- +goose Down
DROP TABLE IF EXISTS run_checkpoint_write;
DROP TABLE IF EXISTS run_checkpoint;
