-- +goose Up
-- What each agent of a run used (#133), as docs/api-contract-m13.md agrees: the model it ran on, with the prices
-- that model had when the run's stream first opened, and its tokens and cost. run_usage keeps the run's totals;
-- the control plane alone writes both, from the runner's llm.usage events, and both are deleted with their run.
CREATE TABLE run_usage_agent (
    run_id        uuid NOT NULL REFERENCES run(id) ON DELETE CASCADE,
    node_id       text NOT NULL CHECK (char_length(node_id) BETWEEN 1 AND 63),
    provider_id   uuid REFERENCES provider(id) ON DELETE SET NULL,
    provider_name text NOT NULL,
    model         text NOT NULL CHECK (btrim(model) <> ''),
    input_tokens  bigint NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens bigint NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cost          numeric(18, 6) NOT NULL DEFAULT 0 CHECK (cost >= 0),
    price_in      numeric(12, 4) NOT NULL DEFAULT 0,
    price_out     numeric(12, 4) NOT NULL DEFAULT 0,
    priced        boolean NOT NULL DEFAULT false,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, node_id)
);

ALTER TABLE run_usage_agent ENABLE ROW LEVEL SECURITY;
ALTER TABLE run_usage_agent FORCE  ROW LEVEL SECURITY;
REVOKE INSERT, UPDATE, DELETE ON run_usage_agent FROM rotor_app;

-- An agent's usage is seen by whoever sees the run, as the run's own.
CREATE POLICY run_usage_agent_read ON run_usage_agent FOR SELECT TO rotor_app
    USING (EXISTS (SELECT 1 FROM run r WHERE r.id = run_usage_agent.run_id));
CREATE POLICY run_usage_agent_admin_bypass ON run_usage_agent TO rotor_admin USING (true) WITH CHECK (true);

-- +goose Down
DROP TABLE IF EXISTS run_usage_agent;
