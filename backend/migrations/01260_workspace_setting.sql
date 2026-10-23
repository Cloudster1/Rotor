-- +goose Up
-- What workspaces may use (#136), as docs/api-contract-m13.md agrees: the toolchain images by the name workflows
-- give, the one package mirror they reach, how long a step may take and how large its work tree is.
CREATE TABLE workspace_setting (
    one            boolean PRIMARY KEY DEFAULT true CHECK (one),
    images         jsonb NOT NULL DEFAULT '[]'
        CONSTRAINT workspace_setting_images_list CHECK (jsonb_typeof(images) = 'array' AND jsonb_array_length(images) <= 20),
    package_mirror text NOT NULL DEFAULT '' CONSTRAINT workspace_setting_mirror_bounded CHECK (length(package_mirror) <= 500),
    max_step_time  text NOT NULL DEFAULT '1h' CONSTRAINT workspace_setting_step_time_bounded CHECK (length(max_step_time) <= 20),
    max_volume     text NOT NULL DEFAULT '1Gi' CONSTRAINT workspace_setting_volume_bounded CHECK (length(max_volume) <= 20),
    changed_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at     timestamptz NOT NULL DEFAULT now()
);

INSERT INTO workspace_setting (one) VALUES (true);
CREATE TRIGGER workspace_setting_updated_at BEFORE UPDATE ON workspace_setting FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE workspace_setting ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_setting FORCE  ROW LEVEL SECURITY;

-- Everyone signed in reads it, since a workflow's images are checked against it when it is saved; administrators
-- alone change it, in person.
CREATE POLICY workspace_setting_read ON workspace_setting FOR SELECT TO rotor_app USING (current_actor_id() IS NOT NULL);
CREATE POLICY workspace_setting_admin_change ON workspace_setting FOR UPDATE TO rotor_app
    USING ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL)
    WITH CHECK ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL);
CREATE POLICY workspace_setting_admin_bypass ON workspace_setting TO rotor_admin USING (true) WITH CHECK (true);
REVOKE ALL ON workspace_setting FROM rotor_app;
GRANT SELECT, UPDATE ON workspace_setting TO rotor_app;

-- +goose Down
DROP TABLE IF EXISTS workspace_setting;
