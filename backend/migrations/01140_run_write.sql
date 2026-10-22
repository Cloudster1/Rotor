-- +goose Up
-- The write ledger (#127), as docs/api-contract-m12.md agrees: what each run wrote through a tool, by the ids its
-- answers carried, so an event about one of them does not start the workflow that made it. The control plane alone
-- writes it, from the runner's tool.called events; it is deleted with its run.
CREATE TABLE run_write (
    run_id  uuid NOT NULL REFERENCES run(id) ON DELETE CASCADE,
    tool_id uuid NOT NULL REFERENCES tool(id) ON DELETE CASCADE,
    call    text NOT NULL CHECK (char_length(call) BETWEEN 1 AND 100),
    subject text NOT NULL CHECK (char_length(subject) BETWEEN 1 AND 500),
    at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, tool_id, subject)
);

CREATE INDEX run_write_subject_idx ON run_write (tool_id, subject);

ALTER TABLE run_write ENABLE ROW LEVEL SECURITY;
ALTER TABLE run_write FORCE  ROW LEVEL SECURITY;
REVOKE INSERT, UPDATE, DELETE ON run_write FROM rotor_app;

-- A run's writes are seen by whoever sees the run.
CREATE POLICY run_write_read ON run_write FOR SELECT TO rotor_app
    USING (EXISTS (SELECT 1 FROM run r WHERE r.id = run_write.run_id));
CREATE POLICY run_write_admin_bypass ON run_write TO rotor_admin USING (true) WITH CHECK (true);

-- +goose Down
DROP TABLE IF EXISTS run_write;
