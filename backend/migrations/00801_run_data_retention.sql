-- +goose Up
-- How long a finished run's data is kept before the worker deletes it (#41): the installation's days here, a
-- workflow's own retention when it is shorter. The audit log is never deleted.
CREATE TABLE retention_setting (
    one           boolean PRIMARY KEY DEFAULT true CHECK (one),
    run_data_days integer NOT NULL DEFAULT 90 CHECK (run_data_days BETWEEN 1 AND 3650),
    changed_by    uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

INSERT INTO retention_setting (one) VALUES (true);
CREATE TRIGGER retention_setting_updated_at BEFORE UPDATE ON retention_setting FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE retention_setting ENABLE ROW LEVEL SECURITY;
ALTER TABLE retention_setting FORCE  ROW LEVEL SECURITY;
REVOKE INSERT, DELETE ON retention_setting FROM rotor_app;

-- Everyone signed in reads how long runs are kept, so a workflow's author sees it; administrators alone change it.
CREATE POLICY retention_setting_read ON retention_setting FOR SELECT TO rotor_app USING (current_actor_id() IS NOT NULL);
CREATE POLICY retention_setting_admin_change ON retention_setting FOR UPDATE TO rotor_app
    USING ((SELECT current_actor_is_admin())) WITH CHECK ((SELECT current_actor_is_admin()));
CREATE POLICY retention_setting_admin_bypass ON retention_setting TO rotor_admin USING (true) WITH CHECK (true);

-- The worker finds the runs past their retention, oldest first.
CREATE INDEX run_finished_idx ON run (finished_at) WHERE finished_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS run_finished_idx;
DROP TABLE IF EXISTS retention_setting;
