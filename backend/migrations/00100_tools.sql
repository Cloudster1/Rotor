-- +goose Up
-- The tools Rotor acts in, and how it is their OAuth client. The client secret lives in OpenBao, never here.

-- Whether the person the transaction acts for administers Rotor; the subquery runs once per statement.
CREATE FUNCTION current_actor_is_admin() RETURNS boolean
    LANGUAGE sql STABLE
    AS $$ SELECT EXISTS (SELECT 1 FROM app_user u WHERE u.id = current_actor_id() AND u.role = 'admin' AND u.is_active) $$;

CREATE TABLE tool (
    id                     uuid PRIMARY KEY DEFAULT uuidv7(),
    name                   text NOT NULL CHECK (btrim(name) <> ''),
    base_url               text NOT NULL UNIQUE,
    resource               text NOT NULL,
    issuer                 text NOT NULL,
    authorization_endpoint text NOT NULL,
    token_endpoint         text NOT NULL,
    revocation_endpoint    text,
    registration_endpoint  text,
    scopes_supported       text[] NOT NULL DEFAULT '{}',
    capabilities           text[] NOT NULL DEFAULT '{}',
    client_id              text NOT NULL,
    client_auth_method     text NOT NULL CHECK (client_auth_method IN ('client_secret_basic', 'client_secret_post')),
    registration           text NOT NULL CHECK (registration IN ('dynamic', 'manual')),
    status                 text NOT NULL DEFAULT 'ready' CHECK (status IN ('ready', 'needs_registration')),
    added_by               uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER tool_updated_at BEFORE UPDATE ON tool FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE tool ENABLE ROW LEVEL SECURITY;
ALTER TABLE tool FORCE  ROW LEVEL SECURITY;

-- Everyone signed in sees every tool, to connect to it; only administrators add, change or remove one.
CREATE POLICY tool_read ON tool FOR SELECT TO rotor_app USING (current_actor_id() IS NOT NULL);
CREATE POLICY tool_admin_add ON tool FOR INSERT TO rotor_app WITH CHECK ((SELECT current_actor_is_admin()));
CREATE POLICY tool_admin_change ON tool FOR UPDATE TO rotor_app
    USING ((SELECT current_actor_is_admin())) WITH CHECK ((SELECT current_actor_is_admin()));
CREATE POLICY tool_admin_remove ON tool FOR DELETE TO rotor_app USING ((SELECT current_actor_is_admin()));
CREATE POLICY tool_admin_bypass ON tool TO rotor_admin USING (true) WITH CHECK (true);

-- +goose Down
DROP TABLE IF EXISTS tool;
DROP FUNCTION IF EXISTS current_actor_is_admin();
