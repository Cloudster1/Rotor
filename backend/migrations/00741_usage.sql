-- +goose Up
-- What runs used (#38): per run, with the prices its model had when the run's stream first opened, and per person
-- and UTC day, for the daily quota. The control plane alone writes them, from the runner's llm.usage events.
CREATE TABLE run_usage (
    run_id        uuid PRIMARY KEY REFERENCES run(id) ON DELETE CASCADE,
    user_id       uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    input_tokens  bigint NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens bigint NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cost          numeric(18, 6) NOT NULL DEFAULT 0 CHECK (cost >= 0),
    price_in      numeric(12, 4) NOT NULL DEFAULT 0,
    price_out     numeric(12, 4) NOT NULL DEFAULT 0,
    priced        boolean NOT NULL DEFAULT false,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE usage_daily (
    user_id       uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    day           date NOT NULL,
    input_tokens  bigint NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens bigint NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cost          numeric(18, 6) NOT NULL DEFAULT 0 CHECK (cost >= 0),
    PRIMARY KEY (user_id, day)
);

CREATE INDEX usage_daily_day_idx ON usage_daily (day);

ALTER TABLE run_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE run_usage FORCE  ROW LEVEL SECURITY;
ALTER TABLE usage_daily ENABLE ROW LEVEL SECURITY;
ALTER TABLE usage_daily FORCE  ROW LEVEL SECURITY;
REVOKE INSERT, UPDATE, DELETE ON run_usage, usage_daily FROM rotor_app;

-- A run's usage is seen by whoever sees the run; a day's by its person and administrators.
CREATE POLICY run_usage_read ON run_usage FOR SELECT TO rotor_app
    USING (EXISTS (SELECT 1 FROM run r WHERE r.id = run_usage.run_id));
CREATE POLICY run_usage_admin_bypass ON run_usage TO rotor_admin USING (true) WITH CHECK (true);
CREATE POLICY usage_daily_read ON usage_daily FOR SELECT TO rotor_app
    USING (user_id = current_actor_id() OR current_actor_is_admin());
CREATE POLICY usage_daily_admin_bypass ON usage_daily TO rotor_admin USING (true) WITH CHECK (true);

-- +goose Down
DROP TABLE IF EXISTS usage_daily;
DROP TABLE IF EXISTS run_usage;
