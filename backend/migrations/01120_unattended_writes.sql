-- +goose Up
-- Named unattended writes (#128), as docs/api-contract-m12.md agrees: the <binding>.<call> pairs a trigger's owner
-- agreed may write with no approval before them, and the administrators' switch that forbids them for everyone.
ALTER TABLE trigger ADD COLUMN allow_unattended text[] NOT NULL DEFAULT '{}'
    CONSTRAINT trigger_allow_unattended_bounded CHECK (cardinality(allow_unattended) <= 400);

CREATE TABLE unattended_setting (
    one        boolean PRIMARY KEY DEFAULT true CHECK (one),
    allowed    boolean NOT NULL DEFAULT true,
    changed_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO unattended_setting (one) VALUES (true);
CREATE TRIGGER unattended_setting_updated_at BEFORE UPDATE ON unattended_setting FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE unattended_setting ENABLE ROW LEVEL SECURITY;
ALTER TABLE unattended_setting FORCE  ROW LEVEL SECURITY;

-- Everyone signed in reads it, since a trigger is checked against it; administrators alone change it, in person.
CREATE POLICY unattended_setting_read ON unattended_setting FOR SELECT TO rotor_app USING (current_actor_id() IS NOT NULL);
CREATE POLICY unattended_setting_admin_change ON unattended_setting FOR UPDATE TO rotor_app
    USING ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL)
    WITH CHECK ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL);
CREATE POLICY unattended_setting_admin_bypass ON unattended_setting TO rotor_admin USING (true) WITH CHECK (true);
REVOKE ALL ON unattended_setting FROM rotor_app;
GRANT SELECT, UPDATE ON unattended_setting TO rotor_app;

-- +goose Down
DROP TABLE IF EXISTS unattended_setting;
ALTER TABLE trigger DROP COLUMN IF EXISTS allow_unattended;
