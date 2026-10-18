-- +goose Up
-- The run's life (#22): who may start one, the moves between its states, and what the worker keeps while it
-- carries a run from queued to its end.
ALTER TABLE run
    -- A retried request with the same key answers the run it started, per person.
    ADD COLUMN idempotency_key text CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    -- Kept on the row because a viewer may no longer see the workflow, nor anyone's row of app_user but their own.
    ADD COLUMN workflow_name   text NOT NULL DEFAULT '',
    ADD COLUMN started_by_name text NOT NULL DEFAULT '',
    -- The worker's lease: a worker that dies holding a run delays it until claimed_until.
    ADD COLUMN claimed_by      text,
    ADD COLUMN claimed_until   timestamptz,
    ADD COLUMN sandbox_id      text,
    -- When the run's current sandbox was started; one that says no Hello within config.StartTimeout is replaced.
    ADD COLUMN sandbox_started_at timestamptz,
    -- How many sandboxes the run was started in; a lost one is replaced until config.MaxRunAttempts.
    ADD COLUMN attempts        integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    -- Set when the runner missed its heartbeats, cleared by its next Hello.
    ADD COLUMN runner_lost_at  timestamptz,
    ADD COLUMN started_at      timestamptz,
    ADD COLUMN finished_at     timestamptz,
    -- The run's own timeout and some grace from when it last started running; the worker ends it after.
    ADD COLUMN deadline_at     timestamptz,
    -- When the worker last cleaned up after the ended run: its sandbox, its credentials and its tokens (#23).
    ADD COLUMN torn_down_at    timestamptz;

CREATE UNIQUE INDEX run_idempotency_idx ON run (user_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX run_live_idx ON run (status, updated_at) WHERE status IN ('queued', 'starting', 'running', 'waiting');

-- +goose StatementBegin
CREATE FUNCTION run_status_move() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status IS NOT DISTINCT FROM OLD.status THEN
        RETURN NEW;
    END IF;
    -- running to starting is a lost sandbox replaced; every other move is the contract's.
    IF (OLD.status, NEW.status) NOT IN (
        ('queued', 'starting'), ('queued', 'cancelled'),
        ('starting', 'running'), ('starting', 'failed'), ('starting', 'cancelled'),
        ('running', 'waiting'), ('running', 'succeeded'), ('running', 'failed'), ('running', 'cancelled'),
        ('running', 'starting'),
        ('waiting', 'starting'), ('waiting', 'cancelled'), ('waiting', 'expired')) THEN
        RAISE EXCEPTION 'a run cannot move from % to %', OLD.status, NEW.status
            USING ERRCODE = 'check_violation', CONSTRAINT = 'run_status_move';
    END IF;
    IF NEW.status = 'running' AND OLD.started_at IS NULL THEN
        NEW.started_at := now();
    END IF;
    IF NEW.status IN ('succeeded', 'failed', 'cancelled', 'expired') THEN
        NEW.finished_at := now();
        NEW.claimed_by := NULL;
        NEW.claimed_until := NULL;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER run_status_move BEFORE UPDATE OF status ON run FOR EACH ROW EXECUTE FUNCTION run_status_move();

-- A person starts their own runs of workflows they may see, always queued; everything after is the control plane's.
GRANT INSERT ON run TO rotor_app;
CREATE POLICY run_own_start ON run FOR INSERT TO rotor_app
    WITH CHECK (user_id = current_actor_id() AND status = 'queued' AND attempts = 0 AND runner_seq = 0
        AND sandbox_id IS NULL AND claimed_by IS NULL AND resume IS NULL AND output IS NULL
        AND EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow_id AND w.archived_at IS NULL));

-- A run is seen by whoever started it, the workflow's owner and administrators; so are its events.
DROP POLICY run_own_read ON run;
DROP POLICY run_event_own_read ON run_event;
-- +goose StatementBegin
CREATE FUNCTION run_visible(starter uuid, workflow uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
    SELECT starter = current_actor_id()
        OR EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow AND w.owner_id = current_actor_id())
        OR current_actor_is_admin()
    $$;
-- +goose StatementEnd
CREATE POLICY run_read ON run FOR SELECT TO rotor_app USING (run_visible(user_id, workflow_id));
CREATE POLICY run_event_read ON run_event FOR SELECT TO rotor_app
    USING (EXISTS (SELECT 1 FROM run r WHERE r.id = run_event.run_id));

-- Where the browser goes once a connection is made, such as back to the workflow it was started from.
ALTER TABLE connection_login ADD COLUMN next_path text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE connection_login DROP COLUMN IF EXISTS next_path;
DROP POLICY IF EXISTS run_event_read ON run_event;
DROP POLICY IF EXISTS run_read ON run;
DROP FUNCTION IF EXISTS run_visible(uuid, uuid);
CREATE POLICY run_own_read ON run FOR SELECT TO rotor_app USING (user_id = current_actor_id());
CREATE POLICY run_event_own_read ON run_event FOR SELECT TO rotor_app
    USING (EXISTS (SELECT 1 FROM run r WHERE r.id = run_event.run_id AND r.user_id = current_actor_id()));
DROP POLICY IF EXISTS run_own_start ON run;
REVOKE INSERT ON run FROM rotor_app;
DROP TRIGGER IF EXISTS run_status_move ON run;
DROP FUNCTION IF EXISTS run_status_move();
DROP INDEX IF EXISTS run_live_idx;
DROP INDEX IF EXISTS run_idempotency_idx;
ALTER TABLE run
    DROP COLUMN IF EXISTS torn_down_at, DROP COLUMN IF EXISTS sandbox_started_at, DROP COLUMN IF EXISTS deadline_at, DROP COLUMN IF EXISTS finished_at, DROP COLUMN IF EXISTS started_at,
    DROP COLUMN IF EXISTS runner_lost_at, DROP COLUMN IF EXISTS attempts, DROP COLUMN IF EXISTS sandbox_id,
    DROP COLUMN IF EXISTS claimed_until, DROP COLUMN IF EXISTS claimed_by, DROP COLUMN IF EXISTS started_by_name,
    DROP COLUMN IF EXISTS workflow_name, DROP COLUMN IF EXISTS idempotency_key;
