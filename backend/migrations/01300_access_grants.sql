-- +goose Up
-- Access is held as roles, groups and grants (#202), as Armature holds it: a role is a named set of permissions, a
-- grant gives one to a person or a group over the whole installation or one workflow. Armature checks grants in its
-- handlers alone; Rotor keeps row level security, so every policy asks the grants through actor_can().

CREATE TABLE role (
    key                    text PRIMARY KEY,
    name                   text NOT NULL,
    description            text NOT NULL DEFAULT '',
    installation_wide_only boolean NOT NULL DEFAULT false,
    builtin                boolean NOT NULL DEFAULT false,
    permissions            text[] NOT NULL DEFAULT '{}',
    sort_order             integer NOT NULL DEFAULT 100,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT role_key_shape CHECK (key ~ '^[a-z][a-z0-9_]{1,39}$'),
    CONSTRAINT role_name_not_blank CHECK (btrim(name) <> ''),
    -- The permissions the code checks for, as perm.AllPermissions lists them; a row naming another would grant a word
    -- nothing checks.
    CONSTRAINT role_known_permissions CHECK (
        permissions <@ ARRAY['workflow.read', 'workflow.run', 'workflow.edit', 'workflow.administer', 'workflow.create',
                             'rotor.administer']::text[]
    )
);
CREATE UNIQUE INDEX role_name_idx ON role (lower(name));
CREATE TRIGGER role_set_updated_at BEFORE UPDATE ON role FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A group is a name for a set of people: the realm's, whose members follow every sign-in; one made by hand; or the
-- built-in Everyone.
CREATE TABLE user_group (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    source       text NOT NULL DEFAULT 'local' CHECK (source IN ('local', 'oidc')),
    -- The value the realm's groups claim is matched against.
    external_ref text,
    builtin      text CONSTRAINT user_group_builtin_known CHECK (builtin IN ('everyone')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_group_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT user_group_external_ref_belongs_to_oidc CHECK (source = 'oidc' OR external_ref IS NULL)
);
CREATE UNIQUE INDEX user_group_name_idx ON user_group (lower(name));
CREATE UNIQUE INDEX user_group_ref_idx ON user_group (external_ref) WHERE external_ref IS NOT NULL;
CREATE UNIQUE INDEX user_group_builtin_idx ON user_group (builtin) WHERE builtin IS NOT NULL;
CREATE TRIGGER user_group_set_updated_at BEFORE UPDATE ON user_group FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE group_member (
    group_id   uuid NOT NULL REFERENCES user_group(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX group_member_user_idx ON group_member (user_id);

-- A grant is a role, a scope and a subject: the scope is one workflow or, when null, the whole installation; the
-- subject is a person or a group, never both.
CREATE TABLE role_assignment (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    role        text NOT NULL REFERENCES role(key) ON DELETE CASCADE,
    workflow_id uuid REFERENCES workflow(id) ON DELETE CASCADE,
    user_id     uuid REFERENCES app_user(id) ON DELETE CASCADE,
    group_id    uuid REFERENCES user_group(id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    created_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    CONSTRAINT role_assignment_one_subject CHECK ((user_id IS NULL) <> (group_id IS NULL))
);
-- The same grant twice is the same grant; NULLS NOT DISTINCT makes that true of installation-wide ones too.
CREATE UNIQUE INDEX role_assignment_unique_idx ON role_assignment (role, workflow_id, user_id, group_id) NULLS NOT DISTINCT;
CREATE INDEX role_assignment_user_idx ON role_assignment (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX role_assignment_group_idx ON role_assignment (group_id) WHERE group_id IS NOT NULL;
CREATE INDEX role_assignment_workflow_idx ON role_assignment (workflow_id) WHERE workflow_id IS NOT NULL;

-- Whether the person the transaction acts for holds a permission on a workflow, or over the whole installation when
-- workflow is null: through a grant to them or to a group of theirs. It reads under the caller's own policies.
-- +goose StatementBegin
CREATE FUNCTION actor_can(permission text, workflow uuid) RETURNS boolean
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

-- The group a realm group name stands for, made on sight; a hand-made group of the same name keeps its name.
-- +goose StatementBegin
CREATE FUNCTION realm_group(ref text) RETURNS uuid
    LANGUAGE plpgsql
AS $$
DECLARE
    found uuid;
BEGIN
    SELECT id INTO found FROM user_group WHERE external_ref = ref;
    IF found IS NULL THEN
        INSERT INTO user_group (name, source, external_ref)
        VALUES (CASE WHEN EXISTS (SELECT 1 FROM user_group WHERE lower(name) = lower(ref)) THEN ref || ' (realm)' ELSE ref END,
                'oidc', ref)
        ON CONFLICT DO NOTHING
        RETURNING id INTO found;
    END IF;
    IF found IS NULL THEN
        SELECT id INTO found FROM user_group WHERE external_ref = ref;
    END IF;
    RETURN found;
END;
$$;
-- +goose StatementEnd

-- What a built-in role is stays: it cannot go, its key cannot change, the administrator keeps administering Rotor,
-- and a role Everyone holds never comes to administer it, or an edit could lock everyone out or let everyone in.
-- +goose StatementBegin
CREATE FUNCTION role_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.builtin THEN
            RAISE EXCEPTION 'a built-in role cannot be deleted' USING ERRCODE = 'check_violation', CONSTRAINT = 'role_guard';
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF NEW.key <> OLD.key OR NEW.builtin <> OLD.builtin THEN
            RAISE EXCEPTION 'a role''s key and standing are fixed' USING ERRCODE = 'check_violation', CONSTRAINT = 'role_guard';
        END IF;
        IF 'rotor.administer' = ANY (NEW.permissions) AND NOT ('rotor.administer' = ANY (OLD.permissions))
           AND EXISTS (SELECT 1 FROM role_assignment a JOIN user_group g ON g.id = a.group_id
                       WHERE a.role = NEW.key AND g.builtin = 'everyone') THEN
            RAISE EXCEPTION 'Everyone holds this role, so it cannot administer Rotor'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_everyone_administers';
        END IF;
    END IF;
    IF NEW.key = 'administrator' AND NOT ('rotor.administer' = ANY (NEW.permissions)) THEN
        RAISE EXCEPTION 'the administrator keeps administering Rotor' USING ERRCODE = 'check_violation', CONSTRAINT = 'role_guard';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER role_check BEFORE INSERT OR UPDATE OR DELETE ON role FOR EACH ROW EXECUTE FUNCTION role_guard();

-- A role that answers for the whole installation is never granted over one workflow, and Everyone never administers.
-- +goose StatementBegin
CREATE FUNCTION role_assignment_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.workflow_id IS NOT NULL AND (SELECT installation_wide_only FROM role WHERE key = NEW.role) THEN
        RAISE EXCEPTION 'that role answers for the whole installation and cannot be granted over one workflow'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_guard';
    END IF;
    IF NEW.group_id IS NOT NULL
       AND EXISTS (SELECT 1 FROM user_group WHERE id = NEW.group_id AND builtin = 'everyone')
       AND (SELECT 'rotor.administer' = ANY (permissions) FROM role WHERE key = NEW.role) THEN
        RAISE EXCEPTION 'Everyone cannot administer Rotor'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_everyone_administers';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER role_assignment_check BEFORE INSERT OR UPDATE ON role_assignment
    FOR EACH ROW EXECUTE FUNCTION role_assignment_guard();

-- The installation always keeps an Administrator grant. perm.Store.Revoke refuses the last one too, under the same
-- lock, so two revokes at once cannot each count the other as the one that stays.
-- +goose StatementBegin
CREATE FUNCTION role_assignment_keeps_an_administrator() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.role = OLD.role THEN
        RETURN NULL;
    END IF;
    PERFORM pg_advisory_xact_lock(hashtext('rotor.administrators'));
    IF NOT EXISTS (SELECT 1 FROM role_assignment WHERE role = 'administrator') THEN
        RAISE EXCEPTION 'this is the last administrator grant, so grant Administrator to someone else first'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_keeps_an_administrator';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER role_assignment_keeps_an_administrator AFTER DELETE OR UPDATE OF role ON role_assignment
    FOR EACH ROW WHEN (OLD.role = 'administrator') EXECUTE FUNCTION role_assignment_keeps_an_administrator();

-- Everyone is kept by the installation: its name, description and standing are fixed, and it is never deleted.
-- +goose StatementBegin
CREATE FUNCTION user_group_builtin_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.builtin IS NOT NULL THEN
            RAISE EXCEPTION 'the built-in group % cannot be deleted', OLD.name USING ERRCODE = 'check_violation';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.builtin IS DISTINCT FROM OLD.builtin
       OR (OLD.builtin IS NOT NULL AND (NEW.name, NEW.description, NEW.source, NEW.external_ref)
           IS DISTINCT FROM (OLD.name, OLD.description, OLD.source, OLD.external_ref)) THEN
        RAISE EXCEPTION 'the built-in group % cannot be changed', OLD.name USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER user_group_builtin_guard BEFORE UPDATE OR DELETE ON user_group
    FOR EACH ROW EXECUTE FUNCTION user_group_builtin_guard();

-- Everyone holds every person who may sign in: nobody switched off joins it, and nobody else leaves it by hand.
-- +goose StatementBegin
CREATE FUNCTION group_member_everyone_guard() RETURNS trigger
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
CREATE TRIGGER group_member_everyone_guard BEFORE INSERT OR UPDATE OR DELETE ON group_member
    FOR EACH ROW EXECUTE FUNCTION group_member_everyone_guard();

-- The built-in roles, as perm.Builtins has them. Author keeps today's rule that anyone signed in creates workflows.
INSERT INTO role (key, name, description, installation_wide_only, permissions, sort_order, builtin)
SELECT seeded.*, true FROM (VALUES
    ('administrator', 'Administrator',
     'Administers Rotor: its tools, providers, settings, audit log, and every workflow and grant in it.',
     true, ARRAY['workflow.read', 'workflow.run', 'workflow.edit', 'workflow.administer', 'workflow.create', 'rotor.administer'], 10),
    ('workflow_administrator', 'Workflow administrator',
     'Shares one workflow, archives it, and sees and cancels everyone''s runs of it.',
     false, ARRAY['workflow.read', 'workflow.run', 'workflow.edit', 'workflow.administer'], 20),
    ('editor', 'Editor',
     'Saves new versions of one workflow and runs it.',
     false, ARRAY['workflow.read', 'workflow.run', 'workflow.edit'], 30),
    ('runner', 'Runner',
     'Sees one workflow and starts runs of it.',
     false, ARRAY['workflow.read', 'workflow.run'], 40),
    ('author', 'Author',
     'Creates workflows of their own.',
     true, ARRAY['workflow.create'], 50)
) AS seeded;

INSERT INTO user_group (name, description, builtin) VALUES ('Everyone', 'Every person who may sign in to Rotor.', 'everyone');

-- Nobody's access changes: the realm groups people had at their last sign-in, and the ones workflows are shared with,
-- become groups with the same members; today's administrators' group holds Administrator.
SELECT realm_group(name) FROM (
    SELECT unnest(groups) AS name FROM app_user
    UNION SELECT unnest(shared_groups) FROM workflow
    UNION SELECT 'rotor-administrators'
) named ORDER BY name;
INSERT INTO group_member (group_id, user_id)
SELECT DISTINCT realm_group(g.name), u.id FROM app_user u, unnest(u.groups) AS g(name)
ON CONFLICT DO NOTHING;
INSERT INTO group_member (group_id, user_id)
SELECT g.id, u.id FROM user_group g, app_user u WHERE g.builtin = 'everyone' AND u.is_active;

INSERT INTO role_assignment (role, group_id) VALUES ('administrator', realm_group('rotor-administrators'));
INSERT INTO role_assignment (role, group_id) SELECT 'author', id FROM user_group WHERE builtin = 'everyone';
INSERT INTO role_assignment (role, workflow_id, user_id) SELECT 'workflow_administrator', id, owner_id FROM workflow;
INSERT INTO role_assignment (role, workflow_id, group_id)
SELECT DISTINCT 'runner', w.id, realm_group(g.name) FROM workflow w, unnest(w.shared_groups) AS g(name) WHERE w.sharing = 'groups';
INSERT INTO role_assignment (role, workflow_id, group_id)
SELECT 'runner', w.id, g.id FROM workflow w, user_group g WHERE w.sharing = 'everyone' AND g.builtin = 'everyone';

-- A person joins Everyone when they are first let in, and leaves it while switched off.
-- +goose StatementBegin
CREATE FUNCTION app_user_joins_everyone() RETURNS trigger
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

-- A workflow's creator administers it, whoever writes the row; the grant is the owner's to lose, not to forget. It is
-- made by the definer because nobody may grant on a workflow before they administer it.
-- +goose StatementBegin
CREATE FUNCTION workflow_owner_administers() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    INSERT INTO role_assignment (role, workflow_id, user_id, created_by)
    VALUES ('workflow_administrator', NEW.id, NEW.owner_id, NEW.owner_id)
    ON CONFLICT DO NOTHING;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER workflow_owner_administers AFTER INSERT ON workflow FOR EACH ROW EXECUTE FUNCTION workflow_owner_administers();

-- Until #203 replaces sharing, a workflow's sharing is its runner grants to groups: changing one rewrites the other,
-- making the realm groups it names on sight. Only someone who administers the workflow gets past the guard below.
-- +goose StatementBegin
CREATE FUNCTION workflow_sharing_grants() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND (NEW.sharing, NEW.shared_groups) IS NOT DISTINCT FROM (OLD.sharing, OLD.shared_groups) THEN
        RETURN NULL;
    END IF;
    DELETE FROM role_assignment WHERE workflow_id = NEW.id AND role = 'runner' AND group_id IS NOT NULL;
    IF NEW.sharing = 'everyone' THEN
        INSERT INTO role_assignment (role, workflow_id, group_id, created_by)
        SELECT 'runner', NEW.id, g.id, current_actor_id() FROM user_group g WHERE g.builtin = 'everyone'
        ON CONFLICT DO NOTHING;
    ELSIF NEW.sharing = 'groups' THEN
        INSERT INTO role_assignment (role, workflow_id, group_id, created_by)
        SELECT 'runner', NEW.id, realm_group(g.name), current_actor_id() FROM unnest(NEW.shared_groups) AS g(name)
        ON CONFLICT DO NOTHING;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER workflow_sharing_grants AFTER INSERT OR UPDATE OF sharing, shared_groups ON workflow
    FOR EACH ROW EXECUTE FUNCTION workflow_sharing_grants();

-- Saving a version takes editing a workflow; sharing and archiving it take administering it. One update policy cannot
-- tell the columns apart, so this does, for whoever a transaction acts for.
-- +goose StatementBegin
CREATE FUNCTION workflow_change_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF current_actor_id() IS NULL THEN
        RETURN NEW;
    END IF;
    IF (NEW.sharing, NEW.shared_groups, NEW.archived_at) IS DISTINCT FROM (OLD.sharing, OLD.shared_groups, OLD.archived_at)
       AND NOT actor_can('workflow.administer', NEW.id) THEN
        RAISE EXCEPTION 'sharing or archiving a workflow takes administering it'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'workflow_change_guard';
    END IF;
    IF (NEW.name, NEW.description, NEW.current_version) IS DISTINCT FROM (OLD.name, OLD.description, OLD.current_version)
       AND NOT actor_can('workflow.edit', NEW.id) THEN
        RAISE EXCEPTION 'changing a workflow takes editing it'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'workflow_change_guard';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER workflow_change_guard BEFORE UPDATE ON workflow FOR EACH ROW EXECUTE FUNCTION workflow_change_guard();

-- Row level security on the new tables. actor_can() reads roles, the caller's own memberships and every grant; a
-- policy here that asked actor_can() while reading would ask itself forever, so reading is decided without it.
ALTER TABLE role ENABLE ROW LEVEL SECURITY;
ALTER TABLE role FORCE  ROW LEVEL SECURITY;
CREATE POLICY role_read ON role FOR SELECT TO rotor_app USING (current_actor_id() IS NOT NULL);
CREATE POLICY role_admin_add ON role FOR INSERT TO rotor_app
    WITH CHECK ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL);
CREATE POLICY role_admin_change ON role FOR UPDATE TO rotor_app
    USING ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL)
    WITH CHECK ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL);
CREATE POLICY role_admin_remove ON role FOR DELETE TO rotor_app
    USING ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL);
CREATE POLICY role_admin_bypass ON role TO rotor_admin USING (true) WITH CHECK (true);

ALTER TABLE user_group ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_group FORCE  ROW LEVEL SECURITY;
CREATE POLICY user_group_read ON user_group FOR SELECT TO rotor_app USING (current_actor_id() IS NOT NULL);
CREATE POLICY user_group_admin_add ON user_group FOR INSERT TO rotor_app
    WITH CHECK ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL);
CREATE POLICY user_group_admin_change ON user_group FOR UPDATE TO rotor_app
    USING ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL)
    WITH CHECK ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL);
CREATE POLICY user_group_admin_remove ON user_group FOR DELETE TO rotor_app
    USING ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL);
CREATE POLICY user_group_admin_bypass ON user_group TO rotor_admin USING (true) WITH CHECK (true);

-- A person sees which groups they are in; membership is written at sign-in, as rotor_admin.
ALTER TABLE group_member ENABLE ROW LEVEL SECURITY;
ALTER TABLE group_member FORCE  ROW LEVEL SECURITY;
REVOKE INSERT, UPDATE, DELETE ON group_member FROM rotor_app;
CREATE POLICY group_member_own_read ON group_member FOR SELECT TO rotor_app USING (user_id = current_actor_id());
CREATE POLICY group_member_admin_bypass ON group_member TO rotor_admin USING (true) WITH CHECK (true);

-- Who holds which role is read by everyone signed in, as in Armature. An administrator of Rotor grants anything, an
-- administrator of a workflow grants on it; neither through an assistant. A grant is given or taken, never edited.
ALTER TABLE role_assignment ENABLE ROW LEVEL SECURITY;
ALTER TABLE role_assignment FORCE  ROW LEVEL SECURITY;
REVOKE UPDATE ON role_assignment FROM rotor_app;
CREATE POLICY role_assignment_read ON role_assignment FOR SELECT TO rotor_app USING (current_actor_id() IS NOT NULL);
CREATE POLICY role_assignment_administer_add ON role_assignment FOR INSERT TO rotor_app
    WITH CHECK (current_actor_via() IS NULL
        AND ((SELECT current_actor_is_admin()) OR (workflow_id IS NOT NULL AND actor_can('workflow.administer', workflow_id))));
CREATE POLICY role_assignment_administer_remove ON role_assignment FOR DELETE TO rotor_app
    USING (current_actor_via() IS NULL
        AND ((SELECT current_actor_is_admin()) OR (workflow_id IS NOT NULL AND actor_can('workflow.administer', workflow_id))));
CREATE POLICY role_assignment_admin_bypass ON role_assignment TO rotor_admin USING (true) WITH CHECK (true);

-- +goose Down
DROP TRIGGER IF EXISTS workflow_change_guard ON workflow;
DROP FUNCTION IF EXISTS workflow_change_guard();
DROP TRIGGER IF EXISTS workflow_sharing_grants ON workflow;
DROP FUNCTION IF EXISTS workflow_sharing_grants();
DROP TRIGGER IF EXISTS workflow_owner_administers ON workflow;
DROP FUNCTION IF EXISTS workflow_owner_administers();
DROP TRIGGER IF EXISTS app_user_joins_everyone ON app_user;
DROP FUNCTION IF EXISTS app_user_joins_everyone();
DROP TABLE IF EXISTS role_assignment;
DROP TABLE IF EXISTS group_member;
DROP TABLE IF EXISTS user_group;
DROP TABLE IF EXISTS role;
DROP FUNCTION IF EXISTS group_member_everyone_guard();
DROP FUNCTION IF EXISTS user_group_builtin_guard();
DROP FUNCTION IF EXISTS role_assignment_keeps_an_administrator();
DROP FUNCTION IF EXISTS role_assignment_guard();
DROP FUNCTION IF EXISTS role_guard();
DROP FUNCTION IF EXISTS realm_group(text);
DROP FUNCTION IF EXISTS actor_can(text, uuid);
