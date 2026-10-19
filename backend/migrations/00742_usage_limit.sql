-- +goose Up
-- The installation's limits on one person (#38): tokens and money a day, and runs going at once; empty is no limit.
CREATE TABLE usage_limit (
    one             boolean PRIMARY KEY DEFAULT true CHECK (one),
    daily_tokens    bigint CHECK (daily_tokens > 0),
    daily_cost      numeric(18, 6) CHECK (daily_cost > 0),
    concurrent_runs integer CHECK (concurrent_runs > 0),
    changed_by      uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at      timestamptz NOT NULL DEFAULT now()
);

INSERT INTO usage_limit (one) VALUES (true);
CREATE TRIGGER usage_limit_updated_at BEFORE UPDATE ON usage_limit FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE usage_limit ENABLE ROW LEVEL SECURITY;
ALTER TABLE usage_limit FORCE  ROW LEVEL SECURITY;
REVOKE INSERT, DELETE ON usage_limit FROM rotor_app;

-- Everyone reads the limits that bind them; administrators alone change them.
CREATE POLICY usage_limit_read ON usage_limit FOR SELECT TO rotor_app USING (current_actor_id() IS NOT NULL);
CREATE POLICY usage_limit_admin_change ON usage_limit FOR UPDATE TO rotor_app
    USING ((SELECT current_actor_is_admin())) WITH CHECK ((SELECT current_actor_is_admin()));
CREATE POLICY usage_limit_admin_bypass ON usage_limit TO rotor_admin USING (true) WITH CHECK (true);

-- +goose Down
DROP TABLE IF EXISTS usage_limit;
