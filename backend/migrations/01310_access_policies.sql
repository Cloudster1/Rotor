-- +goose Up
-- The policies that asked owner, sharing and role ask the grants (#202), through actor_can() of
-- 01300_access_grants.sql.

-- Every policy that asked whether the caller administers Rotor now asks the grants.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_actor_is_admin() RETURNS boolean
    LANGUAGE sql STABLE
    AS $$ SELECT actor_can('rotor.administer', NULL) $$;
-- +goose StatementEnd

DROP POLICY audit_log_admins_read ON audit_log;
CREATE POLICY audit_log_admins_read ON audit_log AS RESTRICTIVE FOR SELECT TO rotor_app
    USING ((SELECT current_actor_is_admin()));

-- A workflow is seen, created, changed and given versions by the grants, not by its owner and sharing.
DROP POLICY workflow_read ON workflow;
DROP POLICY workflow_own_create ON workflow;
DROP POLICY workflow_own_change ON workflow;
DROP FUNCTION workflow_visible(uuid, text, text[]);
CREATE POLICY workflow_read ON workflow FOR SELECT TO rotor_app USING (actor_can('workflow.read', id));
CREATE POLICY workflow_create ON workflow FOR INSERT TO rotor_app
    WITH CHECK (owner_id = current_actor_id() AND actor_can('workflow.create', NULL));
CREATE POLICY workflow_change ON workflow FOR UPDATE TO rotor_app
    USING (actor_can('workflow.edit', id) OR actor_can('workflow.administer', id))
    WITH CHECK (actor_can('workflow.edit', id) OR actor_can('workflow.administer', id));
DROP POLICY workflow_version_own_add ON workflow_version;
CREATE POLICY workflow_version_add ON workflow_version FOR INSERT TO rotor_app
    WITH CHECK (created_by = current_actor_id() AND actor_can('workflow.edit', workflow_id));

-- A run is seen by whoever started it and whoever administers its workflow, administrators of Rotor among them.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION run_visible(starter uuid, workflow uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
    SELECT starter = current_actor_id() OR actor_can('workflow.administer', workflow)
    $$;
-- +goose StatementEnd

-- Starting a run, by hand or through a trigger, takes running the workflow.
DROP POLICY run_own_start ON run;
CREATE POLICY run_own_start ON run FOR INSERT TO rotor_app
    WITH CHECK (user_id = current_actor_id() AND status = 'queued' AND attempts = 0 AND runner_seq = 0
        AND sandbox_id IS NULL AND claimed_by IS NULL AND sealed_output IS NULL
        AND actor_can('workflow.run', workflow_id)
        AND EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow_id AND w.archived_at IS NULL)
        AND (trigger_id IS NULL OR EXISTS (SELECT 1 FROM trigger t WHERE t.id = trigger_id
            AND t.owner_id = current_actor_id() AND t.enabled AND t.workflow_id = run.workflow_id)));
DROP POLICY trigger_own_add ON trigger;
CREATE POLICY trigger_own_add ON trigger FOR INSERT TO rotor_app
    WITH CHECK (owner_id = current_actor_id() AND actor_can('workflow.run', workflow_id)
        AND EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow_id AND w.archived_at IS NULL));

-- +goose Down
DROP POLICY IF EXISTS trigger_own_add ON trigger;
CREATE POLICY trigger_own_add ON trigger FOR INSERT TO rotor_app
    WITH CHECK (owner_id = current_actor_id()
        AND EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow_id AND w.archived_at IS NULL));
DROP POLICY IF EXISTS run_own_start ON run;
CREATE POLICY run_own_start ON run FOR INSERT TO rotor_app
    WITH CHECK (user_id = current_actor_id() AND status = 'queued' AND attempts = 0 AND runner_seq = 0
        AND sandbox_id IS NULL AND claimed_by IS NULL AND sealed_output IS NULL
        AND EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow_id AND w.archived_at IS NULL)
        AND (trigger_id IS NULL OR EXISTS (SELECT 1 FROM trigger t WHERE t.id = trigger_id
            AND t.owner_id = current_actor_id() AND t.enabled AND t.workflow_id = run.workflow_id)));
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION run_visible(starter uuid, workflow uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
    SELECT starter = current_actor_id()
        OR EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow AND w.owner_id = current_actor_id())
        OR current_actor_is_admin()
    $$;
-- +goose StatementEnd

DROP POLICY IF EXISTS workflow_version_add ON workflow_version;
CREATE POLICY workflow_version_own_add ON workflow_version FOR INSERT TO rotor_app
    WITH CHECK (created_by = current_actor_id()
        AND EXISTS (SELECT 1 FROM workflow w WHERE w.id = workflow_id AND w.owner_id = current_actor_id()));
DROP POLICY IF EXISTS workflow_change ON workflow;
DROP POLICY IF EXISTS workflow_create ON workflow;
DROP POLICY IF EXISTS workflow_read ON workflow;
-- +goose StatementBegin
CREATE FUNCTION workflow_visible(owner uuid, sharing text, shared_groups text[]) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
    SELECT owner = current_actor_id()
        OR (current_actor_id() IS NOT NULL AND sharing = 'everyone')
        OR (sharing = 'groups' AND shared_groups && (SELECT u.groups FROM app_user u WHERE u.id = current_actor_id()))
        OR current_actor_is_admin()
    $$;
-- +goose StatementEnd
CREATE POLICY workflow_read ON workflow FOR SELECT TO rotor_app USING (workflow_visible(owner_id, sharing, shared_groups));
CREATE POLICY workflow_own_create ON workflow FOR INSERT TO rotor_app WITH CHECK (owner_id = current_actor_id());
CREATE POLICY workflow_own_change ON workflow FOR UPDATE TO rotor_app
    USING (owner_id = current_actor_id()) WITH CHECK (owner_id = current_actor_id());

DROP POLICY IF EXISTS audit_log_admins_read ON audit_log;
CREATE POLICY audit_log_admins_read ON audit_log AS RESTRICTIVE FOR SELECT TO rotor_app
    USING ((SELECT EXISTS (SELECT 1 FROM app_user u WHERE u.id = current_actor_id() AND u.role = 'admin')));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_actor_is_admin() RETURNS boolean
    LANGUAGE sql STABLE
    AS $$ SELECT EXISTS (SELECT 1 FROM app_user u WHERE u.id = current_actor_id() AND u.role = 'admin' AND u.is_active) $$;
-- +goose StatementEnd
