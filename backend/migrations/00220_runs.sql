-- +goose Up
-- Runs, as much as the runner protocol needs (#18); #22 adds the state machine and the worker's columns.
CREATE TABLE run (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    workflow_id uuid NOT NULL,
    version    integer NOT NULL,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    status     text NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'starting', 'running', 'waiting', 'succeeded', 'failed', 'cancelled', 'expired')),
    reason     text NOT NULL DEFAULT '',
    inputs     jsonb NOT NULL DEFAULT '{}'::jsonb,
    output     jsonb,
    -- The answer a waiting run resumes with, handed to the next sandbox's StartRun.
    resume     jsonb,
    -- The highest seq of the runner's that is stored, so a resent message is ignored and a gap refused.
    runner_seq bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (workflow_id, version) REFERENCES workflow_version (workflow_id, number)
);

CREATE INDEX run_user_idx ON run (user_id, created_at DESC);
CREATE INDEX run_workflow_idx ON run (workflow_id, created_at DESC);
CREATE TRIGGER run_updated_at BEFORE UPDATE ON run FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A sandbox's credential: the bootstrap token it starts with, then the session tokens each reconnect rotates.
CREATE TABLE runner_credential (
    token_hash bytea PRIMARY KEY CHECK (length(token_hash) = 32),
    run_id     uuid NOT NULL REFERENCES run(id) ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN ('bootstrap', 'session')),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX runner_credential_run_idx ON runner_credential (run_id);

-- What happened in a run, numbered by the control plane in the order stored: the runner's events and the control
-- plane's own, such as runner.lost. Stored before it is acknowledged, never changed; seq is the SSE id (#24).
CREATE TABLE run_event (
    run_id  uuid NOT NULL REFERENCES run(id) ON DELETE CASCADE,
    seq     bigint NOT NULL CHECK (seq > 0),
    type    text NOT NULL,
    node    text NOT NULL DEFAULT '',
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, seq)
);

-- What the control plane told a run's runner and must tell again after a reconnect until acknowledged. Grants are
-- never kept here: a token is asked for again rather than stored.
CREATE TABLE run_control (
    run_id   uuid NOT NULL REFERENCES run(id) ON DELETE CASCADE,
    seq      bigint NOT NULL CHECK (seq > 0),
    message  bytea NOT NULL,
    sent_at  timestamptz NOT NULL DEFAULT now(),
    acked_at timestamptz,
    PRIMARY KEY (run_id, seq)
);

ALTER TABLE run ENABLE ROW LEVEL SECURITY;
ALTER TABLE run FORCE  ROW LEVEL SECURITY;
ALTER TABLE run_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE run_event FORCE  ROW LEVEL SECURITY;
ALTER TABLE runner_credential ENABLE ROW LEVEL SECURITY;
ALTER TABLE runner_credential FORCE  ROW LEVEL SECURITY;
ALTER TABLE run_control ENABLE ROW LEVEL SECURITY;
ALTER TABLE run_control FORCE  ROW LEVEL SECURITY;

-- A person reads their own runs and their events; #24 widens this to workflow owners and administrators.
CREATE POLICY run_own_read ON run FOR SELECT TO rotor_app USING (user_id = current_actor_id());
CREATE POLICY run_event_own_read ON run_event FOR SELECT TO rotor_app
    USING (EXISTS (SELECT 1 FROM run r WHERE r.id = run_event.run_id AND r.user_id = current_actor_id()));
CREATE POLICY run_admin_bypass ON run TO rotor_admin USING (true) WITH CHECK (true);
CREATE POLICY run_event_admin_bypass ON run_event TO rotor_admin USING (true) WITH CHECK (true);
CREATE POLICY runner_credential_admin_bypass ON runner_credential TO rotor_admin USING (true) WITH CHECK (true);
CREATE POLICY run_control_admin_bypass ON run_control TO rotor_admin USING (true) WITH CHECK (true);

REVOKE INSERT, UPDATE, DELETE ON run, run_event FROM rotor_app;
REVOKE ALL ON runner_credential, run_control FROM rotor_app;
REVOKE UPDATE, DELETE ON run_event FROM rotor_admin;

-- +goose Down
DROP TABLE IF EXISTS run_control;
DROP TABLE IF EXISTS run_event;
DROP TABLE IF EXISTS runner_credential;
DROP TABLE IF EXISTS run;
