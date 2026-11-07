-- +goose Up
-- The LangSmith setting goes (#256): 01520 made it a trace destination, and the api's start of #246 moved its key out
-- of OpenBao's old path and set carried_at. SQL cannot reach OpenBao, so a key not yet moved would be lost with the
-- table; the migration refuses instead, and the previous version, started once, moves it.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM langsmith_setting WHERE key_set_at IS NOT NULL AND carried_at IS NULL) THEN
        RAISE EXCEPTION 'The LangSmith key has not been moved into its trace destination yet. Start the previous version '
            'of Rotor once with OpenBao reachable, so its api moves the key, then run these migrations again.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP TABLE langsmith_setting;
-- Only the LangSmith routes needed to find the one destination LangSmith became.
DROP INDEX trace_destination_one_carried;

-- +goose Down
-- The table as 00721 and 01520 left it, its one row marked carried over so the start of #246 has nothing to move and
-- its routes find the row they read.
CREATE TABLE langsmith_setting (
    id          boolean PRIMARY KEY DEFAULT true CHECK (id),
    enabled     boolean NOT NULL DEFAULT false,
    project     text NOT NULL DEFAULT 'rotor' CHECK (btrim(project) <> '' AND char_length(project) <= 128),
    key_set_at  timestamptz,
    changed_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    carried_at  timestamptz,
    CHECK (NOT enabled OR key_set_at IS NOT NULL)
);
INSERT INTO langsmith_setting (carried_at) VALUES (now());
CREATE TRIGGER langsmith_setting_updated_at BEFORE UPDATE ON langsmith_setting FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE langsmith_setting ENABLE ROW LEVEL SECURITY;
ALTER TABLE langsmith_setting FORCE  ROW LEVEL SECURITY;
REVOKE INSERT, DELETE ON langsmith_setting FROM rotor_app;
CREATE POLICY langsmith_setting_admin_read ON langsmith_setting FOR SELECT TO rotor_app USING ((SELECT current_actor_is_admin()));
CREATE POLICY langsmith_setting_admin_change ON langsmith_setting FOR UPDATE TO rotor_app
    USING ((SELECT current_actor_is_admin())) WITH CHECK ((SELECT current_actor_is_admin()));
CREATE POLICY langsmith_setting_admin_bypass ON langsmith_setting TO rotor_admin USING (true) WITH CHECK (true);

CREATE UNIQUE INDEX trace_destination_one_carried ON trace_destination (carried_over) WHERE carried_over;
