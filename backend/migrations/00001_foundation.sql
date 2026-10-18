-- +goose Up
-- Foundation: the two runtime roles, the row level security machinery every later table plugs into, and people.

-- IDs come from uuidv7(), which Postgres has built in from version 18 on.
-- +goose StatementBegin
DO $$
BEGIN
    IF current_setting('server_version_num')::int < 180000 THEN
        RAISE EXCEPTION 'Rotor needs PostgreSQL 18 or newer, and this server runs %. Upgrade it and migrate again.',
            current_setting('server_version');
    END IF;
END;
$$;
-- +goose StatementEnd

CREATE EXTENSION IF NOT EXISTS citext;

-- rotor_app is subject to every policy; rotor_admin is exempt by policy rather than by being a superuser.
-- A deployment that created them beforehand, with a password to log in, keeps them.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'rotor_app') THEN
        CREATE ROLE rotor_app NOLOGIN NOSUPERUSER NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'rotor_admin') THEN
        CREATE ROLE rotor_admin NOLOGIN NOSUPERUSER NOBYPASSRLS;
    END IF;
END;
$$;
-- +goose StatementEnd

GRANT USAGE ON SCHEMA public TO rotor_app, rotor_admin;

-- Everything later migrations create is granted to the runtime roles as it is made; each table then narrows it.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO rotor_app, rotor_admin;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO rotor_app, rotor_admin;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT EXECUTE ON FUNCTIONS TO rotor_app, rotor_admin;

-- The person the transaction acts for; NULL binds the app role to nothing at all.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_actor_id() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT NULLIF(current_setting('app.user_id', true), '')::uuid
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- A person as the realm knows them. Role and groups are copied from the identity provider at each sign-in.
CREATE TABLE app_user (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    email      citext NOT NULL UNIQUE,
    name       text NOT NULL,
    role       text NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member')),
    groups     text[] NOT NULL DEFAULT '{}',
    is_active  boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT app_user_email_shape CHECK (position('@' in email) > 1)
);

CREATE TRIGGER app_user_set_updated_at BEFORE UPDATE ON app_user
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- FORCE, not just ENABLE: without it the table owner bypasses its own policies.
ALTER TABLE app_user ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_user FORCE  ROW LEVEL SECURITY;

-- The app role reads the person it acts for and nobody else; sign-in writes people as rotor_admin.
CREATE POLICY app_user_self ON app_user FOR SELECT TO rotor_app USING (id = current_actor_id());
CREATE POLICY app_user_admin_bypass ON app_user TO rotor_admin USING (true) WITH CHECK (true);

REVOKE INSERT, UPDATE, DELETE ON app_user FROM rotor_app;

-- +goose Down
-- The roles and default privileges stay: roles belong to the whole server, and another database may use them.
DROP TABLE IF EXISTS app_user CASCADE;
DROP FUNCTION IF EXISTS set_updated_at();
DROP FUNCTION IF EXISTS current_actor_id();
