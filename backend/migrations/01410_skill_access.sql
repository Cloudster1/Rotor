-- +goose Up
-- Skills take the access model of workflows (#219): skill.* permissions, three built-in roles for one skill, grants
-- scoped to a skill, and actor_can_skill() asked by every policy on the skill tables. A built-in skill is read by
-- everyone signed in and written by nobody through rotor_app.

-- The permissions the code checks for, as perm.AllPermissions lists them, in the matrix's order.
ALTER TABLE role DROP CONSTRAINT role_known_permissions;
ALTER TABLE role ADD CONSTRAINT role_known_permissions CHECK (
    permissions <@ ARRAY['workflow.read', 'workflow.run', 'workflow.edit', 'workflow.administer', 'workflow.create',
                         'skill.read', 'skill.edit', 'skill.administer', 'skill.create', 'rotor.administer']::text[]
);

-- The built-in roles for one skill, as perm.Builtins has them.
INSERT INTO role (key, name, description, installation_wide_only, permissions, sort_order, builtin)
SELECT seeded.*, true FROM (VALUES
    ('skill_administrator', 'Skill administrator',
     'Saves new versions of one skill, shares it and archives it.',
     false, ARRAY['skill.read', 'skill.edit', 'skill.administer'], 60),
    ('skill_editor', 'Skill editor',
     'Saves new versions of one skill.',
     false, ARRAY['skill.read', 'skill.edit'], 70),
    ('skill_reader', 'Skill reader',
     'Reads one skill and its versions, and gives it to the agents of their workflows.',
     false, ARRAY['skill.read'], 80)
) AS seeded;

-- Whoever creates workflows creates skills, and the administrator holds every skill permission over the installation.
-- An administrator may have changed either role, so the new words are added rather than the lists rewritten.
UPDATE role SET permissions = permissions || ARRAY['skill.read', 'skill.edit', 'skill.administer', 'skill.create']::text[]
WHERE key = 'administrator';
UPDATE role SET permissions = permissions || ARRAY['skill.create']::text[]
WHERE key = 'author';

-- A grant answers on one workflow, on one skill, or over the installation when neither is named.
ALTER TABLE role_assignment ADD COLUMN skill_id uuid REFERENCES skill(id) ON DELETE CASCADE;
ALTER TABLE role_assignment ADD CONSTRAINT role_assignment_one_scope CHECK (workflow_id IS NULL OR skill_id IS NULL);
DROP INDEX role_assignment_unique_idx;
CREATE UNIQUE INDEX role_assignment_unique_idx ON role_assignment (role, workflow_id, skill_id, user_id, group_id) NULLS NOT DISTINCT;
CREATE INDEX role_assignment_skill_idx ON role_assignment (skill_id) WHERE skill_id IS NOT NULL;

-- A grant on a skill says nothing about workflows or the installation, so actor_can() passes over it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION actor_can(permission text, workflow uuid) RETURNS boolean
    LANGUAGE sql STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
        WHERE actor_can.permission = ANY (r.permissions)
          AND a.skill_id IS NULL
          AND (a.workflow_id IS NULL OR a.workflow_id = actor_can.workflow)
          AND (a.user_id = current_actor_id()
               OR a.group_id IN (SELECT m.group_id FROM group_member m WHERE m.user_id = current_actor_id())))
       AND EXISTS (SELECT 1 FROM app_user u WHERE u.id = current_actor_id() AND u.is_active AND u.standing IS NOT NULL)
$$;
-- +goose StatementEnd

-- Whether the person the transaction acts for holds a permission on a skill: through a grant on it or over the whole
-- installation, to them or to a group of theirs, as actor_can() answers for a workflow.
-- +goose StatementBegin
CREATE FUNCTION actor_can_skill(permission text, skill uuid) RETURNS boolean
    LANGUAGE sql STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
        WHERE actor_can_skill.permission = ANY (r.permissions)
          AND a.workflow_id IS NULL
          AND (a.skill_id IS NULL OR a.skill_id = actor_can_skill.skill)
          AND (a.user_id = current_actor_id()
               OR a.group_id IN (SELECT m.group_id FROM group_member m WHERE m.user_id = current_actor_id())))
       AND EXISTS (SELECT 1 FROM app_user u WHERE u.id = current_actor_id() AND u.is_active AND u.standing IS NOT NULL)
$$;
-- +goose StatementEnd

-- A role that answers for the whole installation is never granted over one workflow or one skill, Everyone never
-- administers, and a built-in skill, read by everyone already, is never shared.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION role_assignment_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.workflow_id IS NOT NULL OR NEW.skill_id IS NOT NULL) AND (SELECT installation_wide_only FROM role WHERE key = NEW.role) THEN
        RAISE EXCEPTION 'that role answers for the whole installation and cannot be granted over one workflow or skill'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_guard';
    END IF;
    IF NEW.skill_id IS NOT NULL AND EXISTS (SELECT 1 FROM skill WHERE id = NEW.skill_id AND builtin) THEN
        RAISE EXCEPTION 'a built-in skill is read by everyone and shared by nobody'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'role_assignment_builtin_skill';
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

-- Only a grant over the installation administers Rotor, so a grant on a skill never counts as the one that stays.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_user_keeps_an_administrator() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('rotor.administrators'));
    IF EXISTS (SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
               LEFT JOIN group_member m ON m.group_id = a.group_id
               WHERE a.workflow_id IS NULL AND a.skill_id IS NULL AND 'rotor.administer' = ANY (r.permissions)
                 AND (a.user_id = OLD.id OR m.user_id = OLD.id))
       AND NOT EXISTS (SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
                       LEFT JOIN group_member m ON m.group_id = a.group_id
                       JOIN app_user u ON u.id = COALESCE(a.user_id, m.user_id)
                       WHERE a.workflow_id IS NULL AND a.skill_id IS NULL AND 'rotor.administer' = ANY (r.permissions)
                         AND u.is_active AND u.id <> OLD.id) THEN
        RAISE EXCEPTION 'the last active administrator cannot be switched off'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'app_user_keeps_an_administrator';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- A skill's creator administers it, as a workflow's does; a built-in skill has no creator to.
-- +goose StatementBegin
CREATE FUNCTION skill_owner_administers() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.owner_id IS NOT NULL THEN
        INSERT INTO role_assignment (role, skill_id, user_id, created_by)
        VALUES ('skill_administrator', NEW.id, NEW.owner_id, NEW.owner_id)
        ON CONFLICT DO NOTHING;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER skill_owner_administers AFTER INSERT ON skill FOR EACH ROW EXECUTE FUNCTION skill_owner_administers();

-- Saving a version takes editing a skill; archiving it and restoring it take administering it. One update policy
-- cannot tell the columns apart, so this does, as workflow_change_guard does for workflows.
-- +goose StatementBegin
CREATE FUNCTION skill_change_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF current_actor_id() IS NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.archived_at IS DISTINCT FROM OLD.archived_at AND NOT actor_can_skill('skill.administer', NEW.id) THEN
        RAISE EXCEPTION 'archiving a skill or restoring it takes administering it'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'skill_change_guard';
    END IF;
    IF (NEW.description, NEW.current_version) IS DISTINCT FROM (OLD.description, OLD.current_version)
       AND NOT actor_can_skill('skill.edit', NEW.id) THEN
        RAISE EXCEPTION 'changing a skill takes editing it'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'skill_change_guard';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER skill_change_guard BEFORE UPDATE ON skill FOR EACH ROW EXECUTE FUNCTION skill_change_guard();

-- A skill is read by its grants, or by everyone signed in when it is built in; it is made by somebody who may create
-- skills, as its owner; and changed by its grants, never when it is built in.
CREATE POLICY skill_read ON skill FOR SELECT TO rotor_app
    USING ((builtin AND current_actor_id() IS NOT NULL) OR actor_can_skill('skill.read', id));
CREATE POLICY skill_create ON skill FOR INSERT TO rotor_app
    WITH CHECK (NOT builtin AND owner_id = current_actor_id() AND actor_can('skill.create', NULL));
CREATE POLICY skill_change ON skill FOR UPDATE TO rotor_app
    USING (NOT builtin AND (actor_can_skill('skill.edit', id) OR actor_can_skill('skill.administer', id)))
    WITH CHECK (NOT builtin AND (actor_can_skill('skill.edit', id) OR actor_can_skill('skill.administer', id)));

-- A version and its files are read with their skill; the subqueries are held to the skill's own policies.
CREATE POLICY skill_version_read ON skill_version FOR SELECT TO rotor_app
    USING (EXISTS (SELECT 1 FROM skill s WHERE s.id = skill_id));
CREATE POLICY skill_version_add ON skill_version FOR INSERT TO rotor_app
    WITH CHECK (created_by = current_actor_id() AND actor_can_skill('skill.edit', skill_id)
        AND EXISTS (SELECT 1 FROM skill s WHERE s.id = skill_id AND NOT s.builtin));
CREATE POLICY skill_file_read ON skill_file FOR SELECT TO rotor_app
    USING (EXISTS (SELECT 1 FROM skill s WHERE s.id = skill_id));
-- A file joins a version only in the transaction that wrote the version, by its author: once saved, a version's
-- files are all it will ever have.
CREATE POLICY skill_file_add ON skill_file FOR INSERT TO rotor_app
    WITH CHECK (EXISTS (SELECT 1 FROM skill_version v JOIN skill s ON s.id = v.skill_id
        WHERE v.skill_id = skill_file.skill_id AND v.number = skill_file.number AND NOT s.builtin
          AND v.created_by = current_actor_id() AND v.xmin = pg_current_xact_id()::xid));

-- Whoever administers a skill grants on it, as for a workflow; never through an assistant.
DROP POLICY role_assignment_administer_add ON role_assignment;
DROP POLICY role_assignment_administer_remove ON role_assignment;
CREATE POLICY role_assignment_administer_add ON role_assignment FOR INSERT TO rotor_app
    WITH CHECK (current_actor_via() IS NULL
        AND ((SELECT current_actor_is_admin())
             OR (workflow_id IS NOT NULL AND actor_can('workflow.administer', workflow_id))
             OR (skill_id IS NOT NULL AND actor_can_skill('skill.administer', skill_id))));
CREATE POLICY role_assignment_administer_remove ON role_assignment FOR DELETE TO rotor_app
    USING (current_actor_via() IS NULL
        AND ((SELECT current_actor_is_admin())
             OR (workflow_id IS NOT NULL AND actor_can('workflow.administer', workflow_id))
             OR (skill_id IS NOT NULL AND actor_can_skill('skill.administer', skill_id))));

-- +goose Down
DROP POLICY IF EXISTS role_assignment_administer_add ON role_assignment;
DROP POLICY IF EXISTS role_assignment_administer_remove ON role_assignment;
CREATE POLICY role_assignment_administer_add ON role_assignment FOR INSERT TO rotor_app
    WITH CHECK (current_actor_via() IS NULL
        AND ((SELECT current_actor_is_admin()) OR (workflow_id IS NOT NULL AND actor_can('workflow.administer', workflow_id))));
CREATE POLICY role_assignment_administer_remove ON role_assignment FOR DELETE TO rotor_app
    USING (current_actor_via() IS NULL
        AND ((SELECT current_actor_is_admin()) OR (workflow_id IS NOT NULL AND actor_can('workflow.administer', workflow_id))));

DROP POLICY IF EXISTS skill_file_add ON skill_file;
DROP POLICY IF EXISTS skill_file_read ON skill_file;
DROP POLICY IF EXISTS skill_version_add ON skill_version;
DROP POLICY IF EXISTS skill_version_read ON skill_version;
DROP POLICY IF EXISTS skill_change ON skill;
DROP POLICY IF EXISTS skill_create ON skill;
DROP POLICY IF EXISTS skill_read ON skill;
DROP TRIGGER IF EXISTS skill_change_guard ON skill;
DROP FUNCTION IF EXISTS skill_change_guard();
DROP TRIGGER IF EXISTS skill_owner_administers ON skill;
DROP FUNCTION IF EXISTS skill_owner_administers();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_user_keeps_an_administrator() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('rotor.administrators'));
    IF EXISTS (SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
               LEFT JOIN group_member m ON m.group_id = a.group_id
               WHERE a.workflow_id IS NULL AND 'rotor.administer' = ANY (r.permissions)
                 AND (a.user_id = OLD.id OR m.user_id = OLD.id))
       AND NOT EXISTS (SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
                       LEFT JOIN group_member m ON m.group_id = a.group_id
                       JOIN app_user u ON u.id = COALESCE(a.user_id, m.user_id)
                       WHERE a.workflow_id IS NULL AND 'rotor.administer' = ANY (r.permissions)
                         AND u.is_active AND u.id <> OLD.id) THEN
        RAISE EXCEPTION 'the last active administrator cannot be switched off'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'app_user_keeps_an_administrator';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION role_assignment_guard() RETURNS trigger
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

DROP FUNCTION IF EXISTS actor_can_skill(text, uuid);
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

DELETE FROM role_assignment WHERE skill_id IS NOT NULL;
DROP INDEX IF EXISTS role_assignment_skill_idx;
DROP INDEX IF EXISTS role_assignment_unique_idx;
ALTER TABLE role_assignment DROP CONSTRAINT IF EXISTS role_assignment_one_scope;
ALTER TABLE role_assignment DROP COLUMN IF EXISTS skill_id;
CREATE UNIQUE INDEX role_assignment_unique_idx ON role_assignment (role, workflow_id, user_id, group_id) NULLS NOT DISTINCT;

-- The built-in skill roles go, which role_guard refuses to anybody else, and every role forgets the skill words.
ALTER TABLE role DISABLE TRIGGER role_check;
DELETE FROM role WHERE key IN ('skill_administrator', 'skill_editor', 'skill_reader');
ALTER TABLE role ENABLE TRIGGER role_check;
UPDATE role SET permissions = ARRAY(SELECT p FROM unnest(permissions) AS p
    WHERE p NOT IN ('skill.read', 'skill.edit', 'skill.administer', 'skill.create'))
WHERE permissions && ARRAY['skill.read', 'skill.edit', 'skill.administer', 'skill.create']::text[];
ALTER TABLE role DROP CONSTRAINT role_known_permissions;
ALTER TABLE role ADD CONSTRAINT role_known_permissions CHECK (
    permissions <@ ARRAY['workflow.read', 'workflow.run', 'workflow.edit', 'workflow.administer', 'workflow.create',
                         'rotor.administer']::text[]
);
