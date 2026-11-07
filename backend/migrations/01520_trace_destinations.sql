-- +goose Up
-- The OTLP destinations runs' traces are sent to (#246), and the LangSmith setting carried over as one.
-- docs/api-contract-m18.md says what each column means; the numbers below are internal/config/traces.go's, held to it
-- by a test. Header values live in OpenBao, never here.
CREATE TABLE trace_destination (
    id                      uuid PRIMARY KEY DEFAULT uuidv7(),
    name                    text NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 64),
    endpoint                text NOT NULL CHECK (endpoint ~ '^https?://' AND char_length(endpoint) <= 2048),
    header_names            text[] NOT NULL DEFAULT '{}' CHECK (cardinality(header_names) <= 16),
    -- When the header values were put in OpenBao; NULL while none are, and nothing is sent while names wait for them.
    headers_set_at          timestamptz,
    enabled                 boolean NOT NULL DEFAULT false,
    content                 boolean NOT NULL DEFAULT false,
    content_acknowledged_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    content_acknowledged_at timestamptz,
    trace_link              text CHECK (trace_link IS NULL OR (trace_link ~ '^https?://'
        AND position('{traceId}' IN trace_link) > 0 AND char_length(trace_link) <= 2048)),
    carried_over            boolean NOT NULL DEFAULT false,
    changed_by              uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at              timestamptz NOT NULL DEFAULT now(),
    -- Text leaves Rotor for a destination only once an administrator said it may.
    CONSTRAINT trace_destination_content_acknowledged CHECK (NOT content OR content_acknowledged_at IS NOT NULL)
);
CREATE UNIQUE INDEX trace_destination_name_unique ON trace_destination (lower(name));
-- The deprecated LangSmith routes act on the one destination LangSmith became.
CREATE UNIQUE INDEX trace_destination_one_carried ON trace_destination (carried_over) WHERE carried_over;
CREATE TRIGGER trace_destination_updated_at BEFORE UPDATE ON trace_destination FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Each destination is one more export per batch, so a ninth is refused whoever inserts it.
-- +goose StatementBegin
CREATE FUNCTION trace_destination_cap() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    -- One insert at a time, so two administrators adding together cannot pass the cap between them.
    PERFORM pg_advisory_xact_lock(hashtext('trace_destination_cap'));
    IF (SELECT count(*) FROM trace_destination) >= 8 THEN
        RAISE EXCEPTION 'at most 8 trace destinations'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_destination_cap';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER trace_destination_cap BEFORE INSERT ON trace_destination FOR EACH ROW EXECUTE FUNCTION trace_destination_cap();

ALTER TABLE trace_destination ENABLE ROW LEVEL SECURITY;
ALTER TABLE trace_destination FORCE  ROW LEVEL SECURITY;
CREATE POLICY trace_destination_admin ON trace_destination TO rotor_app
    USING ((SELECT current_actor_is_admin())) WITH CHECK ((SELECT current_actor_is_admin()));
-- The api reads every destination to send runs' spans and to link a run's trace for whoever reads the run.
CREATE POLICY trace_destination_admin_bypass ON trace_destination TO rotor_admin USING (true) WITH CHECK (true);

-- LangSmith, where it ever held a key, becomes a destination as it was set; the api's start moves its key and project
-- out of OpenBao's old path into the row's headers, and sets carried_at once it has.
ALTER TABLE langsmith_setting ADD COLUMN carried_at timestamptz;
INSERT INTO trace_destination (name, endpoint, header_names, enabled, content, content_acknowledged_by,
    content_acknowledged_at, carried_over, changed_by, updated_at)
SELECT 'LangSmith', 'https://api.smith.langchain.com/otel/v1/traces', ARRAY['Langsmith-Project', 'x-api-key'], enabled,
    enabled, CASE WHEN enabled THEN changed_by END, CASE WHEN enabled THEN updated_at END, true, changed_by, updated_at
FROM langsmith_setting WHERE key_set_at IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS trace_destination;
DROP FUNCTION IF EXISTS trace_destination_cap();
ALTER TABLE langsmith_setting DROP COLUMN IF EXISTS carried_at;
