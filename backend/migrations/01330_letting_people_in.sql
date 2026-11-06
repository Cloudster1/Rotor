-- +goose Up
-- Signing in is not being let in (#204, ported from Armature's 00901, 00938 and 00947). A person the provider knows
-- waits in join_request until an administrator lets them in, or their realm group's grant does; only a person let in
-- joins Everyone or holds anything. Single sign-on itself moves from the environment into oidc_provider, one row an
-- administrator edits, whose client secret stays in OpenBao.

-- Whether somebody is let in; NULL is waiting or turned away. Rotor's administrators are a grant, so a member is the
-- only standing there is. Everybody known before this change was let in already, so nobody's access changes; the
-- default fills their rows without touching them, and is dropped so a new person starts outside.
ALTER TABLE app_user ADD COLUMN standing text DEFAULT 'member' CONSTRAINT app_user_standing_known CHECK (standing IN ('member'));
ALTER TABLE app_user ALTER COLUMN standing DROP DEFAULT;

-- One request per person; letting them in or turning them away removes it, and a person turned away asks again by
-- signing in again.
CREATE TABLE join_request (
    user_id    uuid PRIMARY KEY REFERENCES app_user(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE join_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE join_request FORCE  ROW LEVEL SECURITY;
CREATE POLICY join_request_admin_bypass ON join_request TO rotor_admin USING (true) WITH CHECK (true);
-- Requests are other people's rows, so Admin's Users page reads and settles them as rotor_admin after asking whether
-- the person it acts for administers Rotor, as it reads people.
REVOKE ALL ON join_request FROM rotor_app;

-- The one identity provider, as Armature keeps it. The secret is in OpenBao: secret_set_at is when it was put there,
-- and NULL while the ROTOR_OIDC_CLIENT_SECRET that seeded the row is the one used.
CREATE TABLE oidc_provider (
    one_row        boolean PRIMARY KEY DEFAULT true CONSTRAINT oidc_provider_one_row CHECK (one_row),
    issuer         text NOT NULL CONSTRAINT oidc_provider_issuer_not_blank CHECK (btrim(issuer) <> ''),
    client_id      text NOT NULL CONSTRAINT oidc_provider_client_id_not_blank CHECK (btrim(client_id) <> ''),
    groups_claim   text NOT NULL DEFAULT 'groups' CONSTRAINT oidc_provider_groups_claim_not_blank CHECK (btrim(groups_claim) <> ''),
    admit_by_group boolean NOT NULL DEFAULT false,
    enabled        boolean NOT NULL DEFAULT true,
    secret_set_at  timestamptz,
    changed_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER oidc_provider_set_updated_at BEFORE UPDATE ON oidc_provider FOR EACH ROW EXECUTE FUNCTION set_updated_at();
ALTER TABLE oidc_provider ENABLE ROW LEVEL SECURITY;
ALTER TABLE oidc_provider FORCE  ROW LEVEL SECURITY;
REVOKE DELETE ON oidc_provider FROM rotor_app;
CREATE POLICY oidc_provider_admin_read ON oidc_provider FOR SELECT TO rotor_app
    USING ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL);
CREATE POLICY oidc_provider_admin_add ON oidc_provider FOR INSERT TO rotor_app
    WITH CHECK ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL);
CREATE POLICY oidc_provider_admin_change ON oidc_provider FOR UPDATE TO rotor_app
    USING ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL)
    WITH CHECK ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL);
CREATE POLICY oidc_provider_admin_bypass ON oidc_provider TO rotor_admin USING (true) WITH CHECK (true);

-- Only a person let in holds anything, whatever was granted to them or their groups.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION actor_can(permission text, workflow uuid) RETURNS boolean
    LANGUAGE sql STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
        WHERE actor_can.permission = ANY (r.permissions)
          AND (a.workflow_id IS NULL OR a.workflow_id = actor_can.workflow)
          AND (a.user_id = current_actor_id()
               OR a.group_id IN (SELECT m.group_id FROM group_member m WHERE m.user_id = current_actor_id())))
       AND EXISTS (SELECT 1 FROM app_user u WHERE u.id = current_actor_id() AND u.is_active AND u.standing IS NOT NULL)
$$;
-- +goose StatementEnd

-- Everyone holds the people let in and not switched off; a person waiting joins it when they are let in.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_user_joins_everyone() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.is_active AND NEW.standing IS NOT NULL THEN
        INSERT INTO group_member (group_id, user_id)
        SELECT id, NEW.id FROM user_group WHERE builtin = 'everyone'
        ON CONFLICT DO NOTHING;
    ELSIF TG_OP = 'UPDATE' THEN
        DELETE FROM group_member m USING user_group g
        WHERE m.group_id = g.id AND g.builtin = 'everyone' AND m.user_id = NEW.id;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
DROP TRIGGER app_user_joins_everyone ON app_user;
CREATE TRIGGER app_user_joins_everyone AFTER INSERT OR UPDATE OF is_active, standing ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_joins_everyone();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION group_member_everyone_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF EXISTS (SELECT 1 FROM user_group WHERE id = OLD.group_id AND builtin = 'everyone')
           AND EXISTS (SELECT 1 FROM app_user WHERE id = OLD.user_id AND is_active AND standing IS NOT NULL) THEN
            RAISE EXCEPTION 'Everyone holds every person who signs in, so nobody is taken out of it by hand'
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN OLD;
    END IF;
    IF EXISTS (SELECT 1 FROM user_group WHERE id = NEW.group_id AND builtin = 'everyone')
       AND NOT EXISTS (SELECT 1 FROM app_user WHERE id = NEW.user_id AND is_active AND standing IS NOT NULL) THEN
        RAISE EXCEPTION 'Everyone holds the people let in, never one switched off or still waiting'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'group_member_everyone_let_in';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION group_member_everyone_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF EXISTS (SELECT 1 FROM user_group WHERE id = OLD.group_id AND builtin = 'everyone')
           AND EXISTS (SELECT 1 FROM app_user WHERE id = OLD.user_id AND is_active) THEN
            RAISE EXCEPTION 'Everyone holds every person who signs in, so nobody is taken out of it by hand'
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN OLD;
    END IF;
    IF EXISTS (SELECT 1 FROM user_group WHERE id = NEW.group_id AND builtin = 'everyone')
       AND NOT EXISTS (SELECT 1 FROM app_user WHERE id = NEW.user_id AND is_active) THEN
        RAISE EXCEPTION 'Everyone holds the people who may sign in, never one switched off' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
DROP TRIGGER IF EXISTS app_user_joins_everyone ON app_user;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_user_joins_everyone() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.is_active THEN
        INSERT INTO group_member (group_id, user_id)
        SELECT id, NEW.id FROM user_group WHERE builtin = 'everyone'
        ON CONFLICT DO NOTHING;
    ELSIF TG_OP = 'UPDATE' THEN
        DELETE FROM group_member m USING user_group g
        WHERE m.group_id = g.id AND g.builtin = 'everyone' AND m.user_id = NEW.id;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER app_user_joins_everyone AFTER INSERT OR UPDATE OF is_active ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_joins_everyone();
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION actor_can(permission text, workflow uuid) RETURNS boolean
    LANGUAGE sql STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
        WHERE actor_can.permission = ANY (r.permissions)
          AND (a.workflow_id IS NULL OR a.workflow_id = actor_can.workflow)
          AND (a.user_id = current_actor_id()
               OR a.group_id IN (SELECT m.group_id FROM group_member m WHERE m.user_id = current_actor_id())))
       AND EXISTS (SELECT 1 FROM app_user u WHERE u.id = current_actor_id() AND u.is_active)
$$;
-- +goose StatementEnd
DROP TABLE IF EXISTS oidc_provider;
DROP TABLE IF EXISTS join_request;
-- Anybody still waiting joins Everyone again by the old rule, as they would have before the change.
INSERT INTO group_member (group_id, user_id)
SELECT g.id, u.id FROM user_group g, app_user u WHERE g.builtin = 'everyone' AND u.is_active
ON CONFLICT DO NOTHING;
ALTER TABLE app_user DROP COLUMN IF EXISTS standing;
