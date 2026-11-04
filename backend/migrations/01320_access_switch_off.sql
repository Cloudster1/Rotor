-- +goose Up
-- Admin's Users page switches people off and on (#203). Switching off the last person who may administer Rotor would
-- leave an installation whose fix is a database console, as taking away the last Administrator grant would, so the
-- database refuses it, under the same lock as revoking, and auth.Service.SwitchOff reads the refusal as a sentence.

-- +goose StatementBegin
CREATE FUNCTION app_user_keeps_an_administrator() RETURNS trigger
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
CREATE TRIGGER app_user_keeps_an_administrator AFTER UPDATE OF is_active ON app_user
    FOR EACH ROW WHEN (OLD.is_active AND NOT NEW.is_active) EXECUTE FUNCTION app_user_keeps_an_administrator();

-- +goose Down
DROP TRIGGER IF EXISTS app_user_keeps_an_administrator ON app_user;
DROP FUNCTION IF EXISTS app_user_keeps_an_administrator();
