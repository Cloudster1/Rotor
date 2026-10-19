-- +goose Up
-- Triggers (#34, #35): a person's schedule or tool event that starts a workflow as them. docs/api-contract-m6.md
-- says what each column means; #35 adds the hooks that feed event triggers.
CREATE TABLE trigger (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    workflow_id     uuid NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    owner_id        uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    name            text NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 200),
    kind            text NOT NULL CHECK (kind IN ('schedule', 'event')),
    enabled         boolean NOT NULL DEFAULT true,
    -- Why Rotor turned the trigger off; empty when it is on or its owner turned it off.
    disabled_reason text NOT NULL DEFAULT '',
    disabled_detail text NOT NULL DEFAULT '',
    disabled_at     timestamptz,
    -- A schedule: five cron fields read in an IANA time zone.
    cron            text,
    time_zone       text,
    no_overlap      boolean NOT NULL DEFAULT false,
    next_run_at     timestamptz,
    -- An event: the tool whose deliveries it matches, and its filters.
    tool_id         uuid REFERENCES tool(id) ON DELETE SET NULL,
    topics          text[] NOT NULL DEFAULT '{}',
    projects        text[] NOT NULL DEFAULT '{}',
    conditions      jsonb NOT NULL DEFAULT '[]'::jsonb,
    -- Fixed values each run starts with.
    inputs          jsonb NOT NULL DEFAULT '{}'::jsonb,
    last_fired_at   timestamptz,
    last_outcome    text NOT NULL DEFAULT '',
    last_reason     text NOT NULL DEFAULT '',
    last_run_id     uuid,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CHECK (kind <> 'schedule' OR (cron IS NOT NULL AND time_zone IS NOT NULL)),
    CHECK (kind <> 'event' OR cardinality(topics) > 0),
    CHECK ((disabled_reason = '') OR NOT enabled)
);

CREATE INDEX trigger_owner_idx ON trigger (owner_id, created_at DESC);
CREATE INDEX trigger_workflow_idx ON trigger (workflow_id);
CREATE INDEX trigger_due_idx ON trigger (next_run_at) WHERE enabled AND kind = 'schedule';
CREATE INDEX trigger_tool_idx ON trigger (tool_id) WHERE enabled AND kind = 'event';
CREATE TRIGGER trigger_updated_at BEFORE UPDATE ON trigger FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- One firing of a trigger: a schedule's due time or a matching event, until the worker started its run or gave up.
-- The key makes a time or an event fire a trigger once, whoever queues it and however often.
CREATE TABLE trigger_fire (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    trigger_id    uuid NOT NULL REFERENCES trigger(id) ON DELETE CASCADE,
    fire_key      text NOT NULL CHECK (fire_key <> ''),
    -- What the run starts with besides the trigger's own inputs, such as the event.
    inputs        jsonb NOT NULL DEFAULT '{}'::jsonb,
    due_at        timestamptz NOT NULL,
    claimed_by    text,
    claimed_until timestamptz,
    attempts      integer NOT NULL DEFAULT 0,
    outcome       text CHECK (outcome IN ('started', 'skipped', 'refused', 'failed')),
    reason        text NOT NULL DEFAULT '',
    run_id        uuid,
    created_at    timestamptz NOT NULL DEFAULT now(),
    done_at       timestamptz,
    UNIQUE (trigger_id, fire_key),
    CHECK ((outcome IS NULL) = (done_at IS NULL))
);

CREATE INDEX trigger_fire_open_idx ON trigger_fire (due_at) WHERE done_at IS NULL;
CREATE INDEX trigger_fire_done_idx ON trigger_fire (done_at) WHERE done_at IS NOT NULL;

-- The trigger that started a run; a person may name only an enabled trigger of their own.
ALTER TABLE run ADD COLUMN trigger_id uuid REFERENCES trigger(id) ON DELETE SET NULL;
DROP POLICY run_own_start ON run;
CREATE POLICY run_own_start ON run FOR INSERT TO rotor_app
    WITH CHECK (user_id = current_actor_id() AND status = 'queued' AND attempts = 0 AND runner_seq = 0
        AND sandbox_id IS NULL AND claimed_by IS NULL AND resume IS NULL AND output IS NULL
        AND EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow_id AND w.archived_at IS NULL)
        AND (trigger_id IS NULL OR EXISTS (SELECT 1 FROM trigger t WHERE t.id = trigger_id
            AND t.owner_id = current_actor_id() AND t.enabled AND t.workflow_id = run.workflow_id)));

ALTER TABLE trigger ENABLE ROW LEVEL SECURITY;
ALTER TABLE trigger FORCE  ROW LEVEL SECURITY;
-- A person keeps their own triggers, on workflows they may see; administrators read and remove anyone's.
CREATE POLICY trigger_read ON trigger FOR SELECT TO rotor_app
    USING (owner_id = current_actor_id() OR current_actor_is_admin());
CREATE POLICY trigger_own_add ON trigger FOR INSERT TO rotor_app
    WITH CHECK (owner_id = current_actor_id()
        AND EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow_id AND w.archived_at IS NULL));
CREATE POLICY trigger_own_change ON trigger FOR UPDATE TO rotor_app
    USING (owner_id = current_actor_id()) WITH CHECK (owner_id = current_actor_id());
CREATE POLICY trigger_remove ON trigger FOR DELETE TO rotor_app
    USING (owner_id = current_actor_id() OR current_actor_is_admin());
CREATE POLICY trigger_admin_bypass ON trigger TO rotor_admin USING (true) WITH CHECK (true);

ALTER TABLE trigger_fire ENABLE ROW LEVEL SECURITY;
ALTER TABLE trigger_fire FORCE  ROW LEVEL SECURITY;
-- Firings are the worker's; a person reads those of triggers they see.
REVOKE INSERT, UPDATE, DELETE ON trigger_fire FROM rotor_app;
CREATE POLICY trigger_fire_read ON trigger_fire FOR SELECT TO rotor_app
    USING (EXISTS (SELECT 1 FROM trigger t WHERE t.id = trigger_fire.trigger_id));
CREATE POLICY trigger_fire_admin_bypass ON trigger_fire TO rotor_admin USING (true) WITH CHECK (true);

-- +goose Down
DROP POLICY IF EXISTS run_own_start ON run;
CREATE POLICY run_own_start ON run FOR INSERT TO rotor_app
    WITH CHECK (user_id = current_actor_id() AND status = 'queued' AND attempts = 0 AND runner_seq = 0
        AND sandbox_id IS NULL AND claimed_by IS NULL AND resume IS NULL AND output IS NULL
        AND EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow_id AND w.archived_at IS NULL));
ALTER TABLE run DROP COLUMN IF EXISTS trigger_id;
DROP TABLE IF EXISTS trigger_fire;
DROP TABLE IF EXISTS trigger;
