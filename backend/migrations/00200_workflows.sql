-- +goose Up
-- Workflows and their versions (#15). A version never changes once written; a run pins one.
CREATE TABLE workflow (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    name            text NOT NULL CHECK (btrim(name) <> ''),
    description     text NOT NULL DEFAULT '',
    owner_id        uuid NOT NULL REFERENCES app_user(id) ON DELETE RESTRICT,
    -- The owner's name as of the first save: a person sees only their own row of app_user.
    owner_name      text NOT NULL DEFAULT '',
    sharing         text NOT NULL DEFAULT 'private' CHECK (sharing IN ('private', 'groups', 'everyone')),
    shared_groups   text[] NOT NULL DEFAULT '{}',
    current_version integer NOT NULL DEFAULT 0 CHECK (current_version >= 0),
    archived_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workflow_groups_named CHECK (sharing <> 'groups' OR cardinality(shared_groups) > 0)
);

CREATE INDEX workflow_owner_idx ON workflow (owner_id);
CREATE INDEX workflow_groups_idx ON workflow USING gin (shared_groups);
CREATE TRIGGER workflow_updated_at BEFORE UPDATE ON workflow FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE workflow_version (
    workflow_id uuid NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    number      integer NOT NULL CHECK (number > 0),
    spec_yaml   text NOT NULL,
    spec        jsonb NOT NULL,
    hash        text NOT NULL,
    comment     text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 500),
    created_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_by_name text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workflow_id, number)
);

-- Versions run 1, 2, 3 with no gaps, as Exciter's revisions do: each is the one after the workflow's current
-- version, which it then becomes. The workflow row is locked, so two saves queue and the later one is refused.
-- +goose StatementBegin
CREATE FUNCTION workflow_version_next() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    current integer;
BEGIN
    SELECT current_version INTO current FROM workflow WHERE id = NEW.workflow_id FOR UPDATE;
    IF current IS NULL OR NEW.number <> current + 1 THEN
        RAISE EXCEPTION 'version % of a workflow is not the one after its current version %', NEW.number, current
            USING ERRCODE = 'check_violation', CONSTRAINT = 'workflow_version_in_sequence';
    END IF;
    UPDATE workflow SET current_version = NEW.number WHERE id = NEW.workflow_id;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER workflow_version_in_sequence BEFORE INSERT ON workflow_version
    FOR EACH ROW EXECUTE FUNCTION workflow_version_next();

-- The owner stays the owner: sharing a workflow never hands it over.
-- +goose StatementBegin
CREATE FUNCTION workflow_owner_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.owner_id <> OLD.owner_id THEN
        RAISE EXCEPTION 'a workflow keeps its owner' USING ERRCODE = 'check_violation', CONSTRAINT = 'workflow_owner_fixed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER workflow_owner_fixed BEFORE UPDATE ON workflow FOR EACH ROW EXECUTE FUNCTION workflow_owner_fixed();

-- Whether the person the transaction acts for may see a workflow: theirs, shared with everyone, shared with one
-- of their realm groups as of their last sign-in, or any for an administrator.
CREATE FUNCTION workflow_visible(owner uuid, sharing text, shared_groups text[]) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
    SELECT owner = current_actor_id()
        OR (current_actor_id() IS NOT NULL AND sharing = 'everyone')
        OR (sharing = 'groups' AND shared_groups && (SELECT u.groups FROM app_user u WHERE u.id = current_actor_id()))
        OR current_actor_is_admin()
    $$;

ALTER TABLE workflow ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow FORCE  ROW LEVEL SECURITY;
CREATE POLICY workflow_read ON workflow FOR SELECT TO rotor_app USING (workflow_visible(owner_id, sharing, shared_groups));
CREATE POLICY workflow_own_create ON workflow FOR INSERT TO rotor_app WITH CHECK (owner_id = current_actor_id());
CREATE POLICY workflow_own_change ON workflow FOR UPDATE TO rotor_app
    USING (owner_id = current_actor_id()) WITH CHECK (owner_id = current_actor_id());
CREATE POLICY workflow_admin_bypass ON workflow TO rotor_admin USING (true) WITH CHECK (true);
-- Workflows are archived, never deleted: runs keep pointing at their versions.
REVOKE DELETE ON workflow FROM rotor_app;

ALTER TABLE workflow_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_version FORCE  ROW LEVEL SECURITY;
-- A version is visible with its workflow; the subquery is held to the workflow's own policies.
CREATE POLICY workflow_version_read ON workflow_version FOR SELECT TO rotor_app
    USING (EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow_id));
CREATE POLICY workflow_version_own_add ON workflow_version FOR INSERT TO rotor_app
    WITH CHECK (created_by = current_actor_id()
        AND EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow_id AND w.owner_id = current_actor_id()));
CREATE POLICY workflow_version_admin_bypass ON workflow_version TO rotor_admin USING (true) WITH CHECK (true);
REVOKE UPDATE, DELETE ON workflow_version FROM rotor_app, rotor_admin;

-- +goose Down
DROP TABLE IF EXISTS workflow_version;
DROP TABLE IF EXISTS workflow;
DROP FUNCTION IF EXISTS workflow_visible(uuid, text, text[]);
DROP FUNCTION IF EXISTS workflow_owner_fixed();
DROP FUNCTION IF EXISTS workflow_version_next();
