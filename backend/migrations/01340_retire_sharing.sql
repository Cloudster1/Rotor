-- +goose Up
-- Grants replaced sharing and the role word (#205, from #203). A workflow's runner grants to groups were its sharing,
-- kept in step by a trigger, and app_user.role was the word the grants gave, written at each sign-in; nothing reads
-- either any more, so both go, and Down rebuilds them from the grants that replaced them.

DROP TRIGGER workflow_sharing_grants ON workflow;
DROP FUNCTION workflow_sharing_grants();

-- Saving a version takes editing a workflow; archiving it, and taking it out of the archive, take administering it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION workflow_change_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF current_actor_id() IS NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.archived_at IS DISTINCT FROM OLD.archived_at AND NOT actor_can('workflow.administer', NEW.id) THEN
        RAISE EXCEPTION 'archiving a workflow or restoring it takes administering it'
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

DROP INDEX workflow_groups_idx;
ALTER TABLE workflow DROP CONSTRAINT workflow_groups_named;
ALTER TABLE workflow DROP COLUMN sharing, DROP COLUMN shared_groups;

ALTER TABLE app_user DROP COLUMN role;

-- +goose Down
-- Holding rotor.administer over the whole installation, directly or through a group, was the word admin.
ALTER TABLE app_user ADD COLUMN role text NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member'));
UPDATE app_user u SET role = 'admin'
WHERE EXISTS (
    SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
    WHERE a.workflow_id IS NULL AND 'rotor.administer' = ANY (r.permissions)
      AND (a.user_id = u.id OR a.group_id IN (SELECT m.group_id FROM group_member m WHERE m.user_id = u.id)));

-- A runner grant to Everyone was sharing with everyone, runner grants to other groups sharing with those groups.
ALTER TABLE workflow
    ADD COLUMN sharing text NOT NULL DEFAULT 'private' CHECK (sharing IN ('private', 'groups', 'everyone')),
    ADD COLUMN shared_groups text[] NOT NULL DEFAULT '{}';
UPDATE workflow w SET sharing = 'everyone'
WHERE EXISTS (SELECT 1 FROM role_assignment a JOIN user_group g ON g.id = a.group_id
              WHERE a.workflow_id = w.id AND a.role = 'runner' AND g.builtin = 'everyone');
UPDATE workflow w SET sharing = 'groups', shared_groups = named.groups
FROM (SELECT a.workflow_id, array_agg(DISTINCT COALESCE(g.external_ref, g.name) ORDER BY COALESCE(g.external_ref, g.name)) AS groups
      FROM role_assignment a JOIN user_group g ON g.id = a.group_id
      WHERE a.role = 'runner' AND a.workflow_id IS NOT NULL AND g.builtin IS NULL
      GROUP BY a.workflow_id) named
WHERE named.workflow_id = w.id AND w.sharing = 'private';
ALTER TABLE workflow ADD CONSTRAINT workflow_groups_named CHECK (sharing <> 'groups' OR cardinality(shared_groups) > 0);
CREATE INDEX workflow_groups_idx ON workflow USING gin (shared_groups);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION workflow_change_guard() RETURNS trigger
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
