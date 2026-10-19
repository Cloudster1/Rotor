-- +goose Up
-- Whether runs' spans also go to LangSmith, and under which project (#37). One row; the key is in OpenBao, never here.
CREATE TABLE langsmith_setting (
    id          boolean PRIMARY KEY DEFAULT true CHECK (id),
    enabled     boolean NOT NULL DEFAULT false,
    project     text NOT NULL DEFAULT 'rotor' CHECK (btrim(project) <> '' AND char_length(project) <= 128),
    -- When the key was put in OpenBao; NULL while there is none.
    key_set_at  timestamptz,
    -- Who last changed it; an administrator's acknowledgement that run data leaves the installation goes with it.
    changed_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (NOT enabled OR key_set_at IS NOT NULL)
);
INSERT INTO langsmith_setting DEFAULT VALUES;
CREATE TRIGGER langsmith_setting_updated_at BEFORE UPDATE ON langsmith_setting FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE langsmith_setting ENABLE ROW LEVEL SECURITY;
ALTER TABLE langsmith_setting FORCE  ROW LEVEL SECURITY;
REVOKE INSERT, DELETE ON langsmith_setting FROM rotor_app;
CREATE POLICY langsmith_setting_admin_read ON langsmith_setting FOR SELECT TO rotor_app USING ((SELECT current_actor_is_admin()));
CREATE POLICY langsmith_setting_admin_change ON langsmith_setting FOR UPDATE TO rotor_app
    USING ((SELECT current_actor_is_admin())) WITH CHECK ((SELECT current_actor_is_admin()));
CREATE POLICY langsmith_setting_admin_bypass ON langsmith_setting TO rotor_admin USING (true) WITH CHECK (true);

-- +goose Down
DROP TABLE IF EXISTS langsmith_setting;
