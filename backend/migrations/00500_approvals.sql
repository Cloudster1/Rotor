-- +goose Up
-- An approval a run waits for (#31): opened when the runner stops at an approval node, decided by the run's starter
-- or the node's group, or ended with its run. docs/api-contract-m5.md says who sees and who decides.
CREATE TABLE approval (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    run_id          uuid NOT NULL REFERENCES run(id) ON DELETE CASCADE,
    -- The run's starter and workflow, copied so visibility never reads run, whose policy reads this table.
    starter_id      uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    workflow_id     uuid NOT NULL,
    workflow_name   text NOT NULL,
    started_by_name text NOT NULL,
    node            text NOT NULL,
    -- LangGraph's id of the interrupt, which the decision answers and nothing else.
    interrupt_id    text NOT NULL CHECK (interrupt_id <> ''),
    message         text NOT NULL,
    preview         jsonb NOT NULL DEFAULT '[]'::jsonb,
    form_schema     jsonb,
    form_values     jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- The group whose members decide; NULL when the starter decides.
    approver_group  text CHECK (approver_group <> ''),
    status          text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'cancelled')),
    decided_by      uuid REFERENCES app_user(id) ON DELETE SET NULL,
    decided_by_name text NOT NULL DEFAULT '',
    comment         text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 2000),
    -- The values as approved, merged over the proposed ones; what the run resumes with.
    approved_values jsonb,
    requested_at    timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz NOT NULL,
    decided_at      timestamptz,
    UNIQUE (run_id, interrupt_id),
    CHECK ((status = 'pending') = (decided_at IS NULL))
);

-- A run waits at one approval at a time; parallel ones are asked one after the other.
CREATE UNIQUE INDEX approval_one_pending_idx ON approval (run_id) WHERE status = 'pending';
CREATE INDEX approval_group_idx ON approval (approver_group, requested_at DESC) WHERE approver_group IS NOT NULL;
CREATE INDEX approval_starter_idx ON approval (starter_id, requested_at DESC);
CREATE INDEX approval_expiry_idx ON approval (expires_at) WHERE status = 'pending';

-- +goose StatementBegin
CREATE FUNCTION approval_status_move() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status IS NOT DISTINCT FROM OLD.status THEN
        RETURN NEW;
    END IF;
    IF OLD.status <> 'pending' THEN
        RAISE EXCEPTION 'an approval that is % cannot become %', OLD.status, NEW.status
            USING ERRCODE = 'check_violation', CONSTRAINT = 'approval_status_move';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER approval_status_move BEFORE UPDATE OF status ON approval FOR EACH ROW EXECUTE FUNCTION approval_status_move();

-- The groups the person acting had at their last sign-in.
CREATE FUNCTION current_actor_groups() RETURNS text[]
    LANGUAGE sql STABLE
    AS $$ SELECT coalesce((SELECT u.groups FROM app_user u WHERE u.id = current_actor_id() AND u.is_active), '{}') $$;

ALTER TABLE approval ENABLE ROW LEVEL SECURITY;
ALTER TABLE approval FORCE  ROW LEVEL SECURITY;
-- People read approvals; only the control plane opens and decides them, after checking the approver.
REVOKE INSERT, UPDATE, DELETE ON approval FROM rotor_app;
CREATE POLICY approval_read ON approval FOR SELECT TO rotor_app
    USING (run_visible(starter_id, workflow_id) OR approver_group = ANY (current_actor_groups()));
CREATE POLICY approval_admin_bypass ON approval TO rotor_admin USING (true) WITH CHECK (true);

-- A group approver sees the run they decide on, and so its events.
CREATE POLICY run_approver_read ON run FOR SELECT TO rotor_app
    USING (EXISTS (SELECT 1 FROM approval a WHERE a.run_id = run.id AND a.approver_group = ANY (current_actor_groups())));

-- +goose Down
DROP POLICY IF EXISTS run_approver_read ON run;
DROP TABLE IF EXISTS approval;
DROP FUNCTION IF EXISTS current_actor_groups();
DROP FUNCTION IF EXISTS approval_status_move();
