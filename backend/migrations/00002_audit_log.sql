-- +goose Up
-- The record of who did what, written in the same transaction as the act and only ever added to.
CREATE TABLE audit_log (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    actor_user_id uuid REFERENCES app_user(id) ON DELETE SET NULL,
    action        text NOT NULL CHECK (btrim(action) <> ''),
    target_type   text NOT NULL,
    target_id     text,
    data          jsonb NOT NULL DEFAULT '{}'::jsonb,
    ip            inet,
    request_id    text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT audit_log_data_object CHECK (jsonb_typeof(data) = 'object')
);

-- Administrators page through it newest first, narrowed by action or actor.
CREATE INDEX audit_log_created_idx ON audit_log (created_at DESC, id DESC);
CREATE INDEX audit_log_action_idx ON audit_log (action, created_at DESC, id DESC);
CREATE INDEX audit_log_actor_idx ON audit_log (actor_user_id, created_at DESC, id DESC);

ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log FORCE  ROW LEVEL SECURITY;

CREATE POLICY audit_log_open ON audit_log TO rotor_app USING (true) WITH CHECK (true);
CREATE POLICY audit_log_admin_bypass ON audit_log TO rotor_admin USING (true) WITH CHECK (true);

-- Only administrators read it. The subquery runs once per statement, not once per row.
CREATE POLICY audit_log_admins_read ON audit_log AS RESTRICTIVE FOR SELECT TO rotor_app
    USING ((SELECT EXISTS (SELECT 1 FROM app_user u WHERE u.id = current_actor_id() AND u.role = 'admin')));

-- An entry names the person the transaction acts for, or nobody when the act is the system's.
CREATE POLICY audit_log_actor_is_caller ON audit_log AS RESTRICTIVE FOR INSERT TO rotor_app
    WITH CHECK (actor_user_id IS NULL OR actor_user_id = current_actor_id());

-- A record that can be rewritten records nothing.
REVOKE UPDATE, DELETE ON audit_log FROM rotor_app, rotor_admin;
GRANT SELECT, INSERT ON audit_log TO rotor_app, rotor_admin;

-- +goose Down
DROP TABLE IF EXISTS audit_log;
