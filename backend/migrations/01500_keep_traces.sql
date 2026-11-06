-- +goose Up
-- Each run's trace kept in Rotor for whoever reads the run (#242), and the traces senders post of no run (#243).
-- docs/api-contract-m18.md says what each column means; the numbers below are internal/config/traces.go's, held to it
-- by a test.

-- A span's trace id finds its run.
ALTER TABLE run ADD COLUMN trace_id text GENERATED ALWAYS AS (substr(traceparent, 4, 32)) STORED;
CREATE INDEX run_trace_id ON run (trace_id) WHERE trace_id <> '';

CREATE TABLE trace_setting (
    id             boolean PRIMARY KEY DEFAULT true CHECK (id),
    retention_days integer NOT NULL DEFAULT 14 CHECK (retention_days BETWEEN 1 AND 90),
    keep_content   boolean NOT NULL DEFAULT false,
    changed_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at     timestamptz NOT NULL DEFAULT now()
);
INSERT INTO trace_setting DEFAULT VALUES;
CREATE TRIGGER trace_setting_updated_at BEFORE UPDATE ON trace_setting FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE trace_setting ENABLE ROW LEVEL SECURITY;
ALTER TABLE trace_setting FORCE  ROW LEVEL SECURITY;
REVOKE INSERT, DELETE ON trace_setting FROM rotor_app;
-- Everyone signed in reads it, so the run's page can say how long its trace is kept.
CREATE POLICY trace_setting_read ON trace_setting FOR SELECT TO rotor_app USING (current_actor_id() IS NOT NULL);
CREATE POLICY trace_setting_admin_change ON trace_setting FOR UPDATE TO rotor_app
    USING ((SELECT current_actor_is_admin())) WITH CHECK ((SELECT current_actor_is_admin()));
CREATE POLICY trace_setting_admin_bypass ON trace_setting TO rotor_admin USING (true) WITH CHECK (true);

-- A trace is a run's or, for spans of no run the sender reads, the sender's own; never both.
CREATE TABLE trace (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    trace_id     text NOT NULL CHECK (trace_id ~ '^[0-9a-f]{32}$' AND trace_id <> repeat('0', 32)),
    run_id       uuid REFERENCES run(id) ON DELETE CASCADE,
    owner_id     uuid REFERENCES app_user(id) ON DELETE CASCADE,
    -- An owned trace's own key; a run's trace seals with the run's.
    data_key     text CHECK (data_key LIKE 'vault:v%:%'),
    spans        integer NOT NULL DEFAULT 0,
    bytes        bigint NOT NULL DEFAULT 0,
    dropped      integer NOT NULL DEFAULT 0,
    cut_at       timestamptz,
    content      boolean NOT NULL DEFAULT false,
    started_at   timestamptz NOT NULL DEFAULT now(),
    last_span_at timestamptz NOT NULL DEFAULT now(),
    expired_at   timestamptz,
    CHECK ((run_id IS NULL) <> (owner_id IS NULL)),
    CHECK (run_id IS NULL OR data_key IS NULL)
);
CREATE UNIQUE INDEX trace_of_run ON trace (run_id) WHERE run_id IS NOT NULL;
CREATE UNIQUE INDEX trace_run_trace_id ON trace (trace_id) WHERE run_id IS NOT NULL;
CREATE UNIQUE INDEX trace_owned_trace_id ON trace (owner_id, trace_id) WHERE owner_id IS NOT NULL;
CREATE INDEX trace_kept_since ON trace (last_span_at) WHERE expired_at IS NULL;

CREATE TABLE trace_span (
    trace          uuid NOT NULL REFERENCES trace(id) ON DELETE CASCADE,
    span_id        text NOT NULL CHECK (span_id ~ '^[0-9a-f]{16}$' AND span_id <> repeat('0', 16)),
    parent_span_id text NOT NULL DEFAULT '' CHECK (parent_span_id = '' OR parent_span_id ~ '^[0-9a-f]{16}$'),
    name           text NOT NULL CHECK (name <> '' AND char_length(name) <= 128),
    kind           text NOT NULL DEFAULT 'UNKNOWN' CHECK (kind IN ('CHAIN', 'LLM', 'TOOL', 'AGENT', 'RETRIEVER', 'EMBEDDING',
        'RERANKER', 'GUARDRAIL', 'EVALUATOR', 'PROMPT', 'UNKNOWN')),
    service        text NOT NULL CHECK (service <> '' AND char_length(service) <= 128),
    source         text NOT NULL CHECK (source IN ('runner', 'otlp')),
    sender_id      uuid REFERENCES app_user(id) ON DELETE SET NULL,
    sender_name    text,
    started_at     timestamptz NOT NULL,
    ended_at       timestamptz NOT NULL CHECK (ended_at >= started_at),
    status         text NOT NULL DEFAULT 'unset' CHECK (status IN ('unset', 'ok', 'error')),
    status_message text NOT NULL DEFAULT '' CHECK (char_length(status_message) <= 512),
    -- Attributes without text; the text ones are sealed_content, one JSON object sealed with the trace's key.
    attributes     jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(attributes) = 'object'),
    events         jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(events) = 'array'),
    sealed_content bytea,
    bytes          integer NOT NULL DEFAULT 0,
    received_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (trace, span_id),
    CHECK (source = 'otlp' OR (sender_id IS NULL AND sender_name IS NULL))
);

-- Whether a trace has taken no span for as long as the installation keeps traces; the sweep and its policies ask it.
-- +goose StatementBegin
CREATE FUNCTION trace_due(last_span_at timestamptz) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$ SELECT last_span_at < now() - make_interval(days => (SELECT retention_days FROM trace_setting)) $$;
-- +goose StatementEnd

-- The cap holds whoever inserts: a span's bytes come from the row itself, and a span past the trace's 10000 spans or
-- 32 MiB, or past 100000 spans of one sender's own traces, is skipped and counted, as is a span id taken already.
-- +goose StatementBegin
CREATE FUNCTION trace_span_cap() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    t trace%ROWTYPE;
BEGIN
    NEW.bytes := octet_length(NEW.name) + octet_length(NEW.status_message) + octet_length(NEW.attributes::text)
        + octet_length(NEW.events::text) + coalesce(octet_length(NEW.sealed_content), 0);
    NEW.received_at := now();
    -- One span at a time per trace, so the counts hold and a span id is taken once.
    SELECT * INTO t FROM trace WHERE id = NEW.trace FOR UPDATE;
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;
    IF EXISTS (SELECT 1 FROM trace_span s WHERE s.trace = NEW.trace AND s.span_id = NEW.span_id) THEN
        RETURN NULL;
    END IF;
    IF t.spans >= 10000 OR t.bytes + NEW.bytes > 33554432
        OR (t.owner_id IS NOT NULL
            AND (SELECT coalesce(sum(o.spans), 0) FROM trace o WHERE o.owner_id = t.owner_id) >= 100000) THEN
        UPDATE trace SET dropped = dropped + 1, cut_at = coalesce(cut_at, now()) WHERE id = t.id;
        RETURN NULL;
    END IF;
    UPDATE trace SET spans = spans + 1, bytes = bytes + NEW.bytes, last_span_at = now(), expired_at = NULL,
        content = content OR NEW.sealed_content IS NOT NULL
        WHERE id = t.id;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER trace_span_cap BEFORE INSERT ON trace_span FOR EACH ROW EXECUTE FUNCTION trace_span_cap();

ALTER TABLE trace ENABLE ROW LEVEL SECURITY;
ALTER TABLE trace FORCE  ROW LEVEL SECURITY;
ALTER TABLE trace_span ENABLE ROW LEVEL SECURITY;
ALTER TABLE trace_span FORCE  ROW LEVEL SECURITY;
-- Spans are immutable; the counts on a trace move by the trigger alone, and only the sweep deletes.
REVOKE UPDATE, DELETE ON trace FROM rotor_app;
REVOKE UPDATE, DELETE ON trace_span FROM rotor_app;
REVOKE UPDATE ON trace_span FROM rotor_admin;

-- A run's trace is read exactly when the run's events are, so run_visible decides both; an owned trace by its sender
-- and administrators of Rotor.
CREATE POLICY trace_read ON trace FOR SELECT TO rotor_app
    USING (CASE WHEN run_id IS NOT NULL THEN EXISTS (SELECT 1 FROM run r WHERE r.id = trace.run_id)
        ELSE owner_id = current_actor_id() OR (SELECT current_actor_is_admin()) END);
-- An empty trace of a run the caller reads, under the run's own trace id, or of the caller's own.
CREATE POLICY trace_add ON trace FOR INSERT TO rotor_app
    WITH CHECK (spans = 0 AND bytes = 0 AND dropped = 0 AND cut_at IS NULL AND NOT content AND expired_at IS NULL
        AND CASE WHEN run_id IS NOT NULL THEN data_key IS NULL
                AND EXISTS (SELECT 1 FROM run r WHERE r.id = trace.run_id AND r.trace_id = trace.trace_id)
            ELSE owner_id = current_actor_id() END);
CREATE POLICY trace_span_read ON trace_span FOR SELECT TO rotor_app
    USING (EXISTS (SELECT 1 FROM trace t WHERE t.id = trace_span.trace));
-- A sender's span joins a run's trace it reads or its own; the runner's are written as rotor_admin after its checks.
CREATE POLICY trace_span_add ON trace_span FOR INSERT TO rotor_app
    WITH CHECK (source = 'otlp' AND sender_id = current_actor_id()
        AND EXISTS (SELECT 1 FROM trace t WHERE t.id = trace_span.trace
            AND (t.run_id IS NOT NULL OR t.owner_id = current_actor_id())));

CREATE POLICY trace_admin_read ON trace FOR SELECT TO rotor_admin USING (true);
CREATE POLICY trace_admin_add ON trace FOR INSERT TO rotor_admin WITH CHECK (true);
CREATE POLICY trace_admin_change ON trace FOR UPDATE TO rotor_admin USING (true) WITH CHECK (true);
-- The sweep deletes an owned trace whole once it is due; a run's trace goes with its run.
CREATE POLICY trace_admin_sweep ON trace FOR DELETE TO rotor_admin USING (owner_id IS NOT NULL AND trace_due(last_span_at));
CREATE POLICY trace_span_admin_read ON trace_span FOR SELECT TO rotor_admin USING (true);
CREATE POLICY trace_span_admin_add ON trace_span FOR INSERT TO rotor_admin WITH CHECK (true);
CREATE POLICY trace_span_admin_sweep ON trace_span FOR DELETE TO rotor_admin
    USING (EXISTS (SELECT 1 FROM trace t WHERE t.id = trace_span.trace AND trace_due(t.last_span_at)));

-- +goose Down
DROP TABLE IF EXISTS trace_span;
DROP FUNCTION IF EXISTS trace_span_cap();
DROP TABLE IF EXISTS trace;
DROP FUNCTION IF EXISTS trace_due(timestamptz);
DROP TABLE IF EXISTS trace_setting;
DROP INDEX IF EXISTS run_trace_id;
ALTER TABLE run DROP COLUMN IF EXISTS trace_id;
