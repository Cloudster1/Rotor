-- +goose Up
-- The time zone a person reads dates in (#206), kept on the person as Armature keeps it; NULL follows their browser.
-- 64 is twice the longest name of the IANA database, so a real zone always fits and nothing else grows the row.
ALTER TABLE app_user ADD COLUMN time_zone text
    CONSTRAINT app_user_time_zone_bounded CHECK (time_zone IS NULL OR (btrim(time_zone) <> '' AND length(time_zone) <= 64));

-- The realm gives a person everything else on their row, so the time zone is the one column they change, on their own
-- row alone and in person, never through an assistant.
GRANT UPDATE (time_zone) ON app_user TO rotor_app;
CREATE POLICY app_user_self_change ON app_user FOR UPDATE TO rotor_app
    USING (id = current_actor_id() AND current_actor_via() IS NULL)
    WITH CHECK (id = current_actor_id() AND current_actor_via() IS NULL);

-- +goose Down
DROP POLICY IF EXISTS app_user_self_change ON app_user;
REVOKE UPDATE (time_zone) ON app_user FROM rotor_app;
ALTER TABLE app_user DROP COLUMN IF EXISTS time_zone;
