-- +goose Up
-- Rotor as the authorization server of its own MCP endpoint (#47), as docs/api-contract-m10.md agrees: an
-- assistant registers, its person consents, and it gets tokens for MCP alone. Requests, codes and tokens are used
-- before anybody is signed in, so the admin role alone reads them, as it reads sessions; a grant is its owner's and
-- the administrators'. Adapted from Exciter's 00800_oauth.sql at 3b340ca, without projects or metadata documents.

-- The grant an assistant acts through, for the length of a transaction; NULL when a person acts in the browser.
-- +goose StatementBegin
CREATE FUNCTION current_actor_via() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT NULLIF(current_setting('app.via', true), '')::uuid
$$;
-- +goose StatementEnd

-- Whether assistants may connect at all: administrators switch it off for the whole installation.
CREATE TABLE assistant_setting (
    one            boolean PRIMARY KEY DEFAULT true CHECK (one),
    connections_on boolean NOT NULL DEFAULT true,
    changed_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at     timestamptz NOT NULL DEFAULT now()
);

INSERT INTO assistant_setting (one) VALUES (true);
CREATE TRIGGER assistant_setting_updated_at BEFORE UPDATE ON assistant_setting FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A client is the id Rotor gave it when it registered (RFC 7591).
CREATE TABLE oauth_client (
    id              text PRIMARY KEY CHECK (char_length(id) BETWEEN 1 AND 200),
    name            text NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    uri             text NOT NULL DEFAULT '',
    redirect_uris   text[] NOT NULL CHECK (cardinality(redirect_uris) BETWEEN 1 AND 10),
    auth_method     text NOT NULL DEFAULT 'none' CHECK (auth_method IN ('none', 'client_secret_basic', 'client_secret_post')),
    secret_hash     bytea CHECK (octet_length(secret_hash) = 32),
    grant_types     text[] NOT NULL CHECK (grant_types <@ ARRAY['authorization_code', 'refresh_token']::text[]),
    -- The address a registration came from, which its rate is counted by.
    registered_from text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    -- When its last grant ended, which keeps a client that was connected for a while after; null until then.
    idle_since      timestamptz,
    CONSTRAINT oauth_client_secret_when_asked CHECK ((auth_method = 'none') = (secret_hash IS NULL))
);

CREATE INDEX oauth_client_registered_idx ON oauth_client (registered_from, created_at);

-- An authorization waiting for its person, under an id as random as a session's, for RequestTTL.
CREATE TABLE oauth_request (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id      text NOT NULL REFERENCES oauth_client(id) ON DELETE CASCADE,
    redirect_uri   text NOT NULL,
    state          text NOT NULL,
    code_challenge text NOT NULL,
    read_only      boolean NOT NULL,
    resource       text NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL
);

-- What a person let one client do: a client holds one grant for each person.
CREATE TABLE oauth_grant (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id      uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    client_id    text NOT NULL REFERENCES oauth_client(id) ON DELETE CASCADE,
    redirect_uri text NOT NULL,
    read_only    boolean NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    CONSTRAINT oauth_grant_one_per_person UNIQUE (client_id, user_id)
);

CREATE INDEX oauth_grant_user_idx ON oauth_grant (user_id, created_at DESC);

-- A code is used once, for CodeTTL; a used one is kept until then, so its second use ends the grant it made.
CREATE TABLE oauth_code (
    code_hash      bytea PRIMARY KEY CHECK (octet_length(code_hash) = 32),
    grant_id       uuid NOT NULL REFERENCES oauth_grant(id) ON DELETE CASCADE,
    redirect_uri   text NOT NULL,
    code_challenge text NOT NULL,
    resource       text NOT NULL,
    expires_at     timestamptz NOT NULL,
    used_at        timestamptz
);

-- An access token for AccessTTL, or a refresh token, which a newer one replaces; one used once replaced is a
-- stolen one, and ends the grant.
CREATE TABLE oauth_token (
    token_hash  bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    grant_id    uuid NOT NULL REFERENCES oauth_grant(id) ON DELETE CASCADE,
    kind        text NOT NULL CHECK (kind IN ('access', 'refresh')),
    read_only   boolean NOT NULL,
    expires_at  timestamptz NOT NULL,
    replaced_at timestamptz CHECK (replaced_at IS NULL OR kind = 'refresh'),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX oauth_token_grant_idx ON oauth_token (grant_id);

-- +goose StatementBegin
-- What a grant is stays what it was made: only its use is noted, and widening one is consenting again.
CREATE FUNCTION oauth_grant_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.user_id, NEW.client_id, NEW.redirect_uri, NEW.read_only, NEW.created_at)
       IS DISTINCT FROM (OLD.user_id, OLD.client_id, OLD.redirect_uri, OLD.read_only, OLD.created_at) THEN
        RAISE EXCEPTION 'oauth_grant_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'A grant stays what it was made; connect the assistant again instead.';
    END IF;
    RETURN NEW;
END;
$$;

-- A client whose last grant ended is idle from then, which the worker counts its removal from.
CREATE FUNCTION oauth_grant_ended() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    UPDATE oauth_client SET idle_since = now()
    WHERE id = OLD.client_id AND NOT EXISTS (SELECT 1 FROM oauth_grant g WHERE g.client_id = OLD.client_id);
    RETURN NULL;
END;
$$;

-- Switching somebody off ends their grants, as it ends their sessions.
CREATE FUNCTION app_user_ends_grants() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    DELETE FROM oauth_grant WHERE user_id = NEW.id;
    RETURN NULL;
END;
$$;

-- A request decided is taken whole, by whoever consents, though the app role cannot read requests: the id is what
-- the consent page was handed, and an expired request is gone.
CREATE FUNCTION oauth_request_take(request uuid) RETURNS SETOF oauth_request
    LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    DELETE FROM oauth_request WHERE id = request AND expires_at > now() RETURNING *
$$;

-- An approval is decided by a person in Rotor's pages, never by an assistant acting for them, whichever role the
-- transaction runs as; cancelling and expiring stay the system's.
CREATE FUNCTION approval_decided_in_person() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF current_actor_via() IS NOT NULL THEN
        RAISE EXCEPTION 'an assistant cannot decide an approval' USING ERRCODE = 'insufficient_privilege',
            CONSTRAINT = 'approval_decided_in_person';
    END IF;
    RETURN NEW;
END;
$$;

-- The grant an assistant acted through and its client's name, for an audit entry it caused; the grant is checked
-- to be the acting person's, so a forged setting names nothing.
CREATE FUNCTION audit_log_via() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    SELECT g.id, c.name INTO NEW.via_grant, NEW.via_client
    FROM oauth_grant g JOIN oauth_client c ON c.id = g.client_id
    WHERE g.id = current_actor_via() AND g.user_id = current_actor_id();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER oauth_grant_fixed BEFORE UPDATE ON oauth_grant
    FOR EACH ROW EXECUTE FUNCTION oauth_grant_fixed();
CREATE TRIGGER oauth_grant_ended AFTER DELETE ON oauth_grant
    FOR EACH ROW EXECUTE FUNCTION oauth_grant_ended();
CREATE TRIGGER app_user_ends_grants AFTER UPDATE OF is_active ON app_user
    FOR EACH ROW WHEN (OLD.is_active AND NOT NEW.is_active)
    EXECUTE FUNCTION app_user_ends_grants();
CREATE TRIGGER approval_decided_in_person BEFORE UPDATE OF status ON approval
    FOR EACH ROW WHEN (OLD.status = 'pending' AND NEW.status IN ('approved', 'rejected'))
    EXECUTE FUNCTION approval_decided_in_person();

-- An entry made through an assistant says which; no foreign key, so an ended grant keeps its name in the log.
ALTER TABLE audit_log ADD COLUMN via_grant uuid, ADD COLUMN via_client text,
    ADD CONSTRAINT audit_log_via_whole CHECK ((via_grant IS NULL) = (via_client IS NULL));
CREATE TRIGGER audit_log_via BEFORE INSERT ON audit_log
    FOR EACH ROW WHEN (current_actor_via() IS NOT NULL) EXECUTE FUNCTION audit_log_via();

ALTER TABLE assistant_setting ENABLE ROW LEVEL SECURITY;
ALTER TABLE assistant_setting FORCE  ROW LEVEL SECURITY;
ALTER TABLE oauth_client ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_client FORCE  ROW LEVEL SECURITY;
ALTER TABLE oauth_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_request FORCE  ROW LEVEL SECURITY;
ALTER TABLE oauth_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_grant FORCE  ROW LEVEL SECURITY;
ALTER TABLE oauth_code ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_code FORCE  ROW LEVEL SECURITY;
ALTER TABLE oauth_token ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_token FORCE  ROW LEVEL SECURITY;

-- Everyone signed in reads whether assistants may connect; administrators alone change it, in person.
CREATE POLICY assistant_setting_read ON assistant_setting FOR SELECT TO rotor_app USING (current_actor_id() IS NOT NULL);
CREATE POLICY assistant_setting_admin_change ON assistant_setting FOR UPDATE TO rotor_app
    USING ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL)
    WITH CHECK ((SELECT current_actor_is_admin()) AND current_actor_via() IS NULL);
CREATE POLICY assistant_setting_admin_bypass ON assistant_setting TO rotor_admin USING (true) WITH CHECK (true);

-- A client is shown to those signed in, beside the grants that name it.
CREATE POLICY oauth_client_seen_signed_in ON oauth_client FOR SELECT TO rotor_app USING (current_actor_id() IS NOT NULL);
CREATE POLICY oauth_client_admin_bypass ON oauth_client TO rotor_admin USING (true) WITH CHECK (true);
CREATE POLICY oauth_request_admin_bypass ON oauth_request TO rotor_admin USING (true) WITH CHECK (true);

-- A grant is its owner's to see, make and end, and the administrators' to see and end; an assistant makes and ends
-- none, so a grant is always a person's own doing.
CREATE POLICY oauth_grant_seen_by_owner ON oauth_grant FOR SELECT TO rotor_app
    USING (user_id = current_actor_id() OR (SELECT current_actor_is_admin()));
CREATE POLICY oauth_grant_made_by_owner ON oauth_grant FOR INSERT TO rotor_app
    WITH CHECK (user_id = current_actor_id() AND last_used_at IS NULL AND current_actor_via() IS NULL);
CREATE POLICY oauth_grant_ended_by_owner ON oauth_grant FOR DELETE TO rotor_app
    USING ((user_id = current_actor_id() OR (SELECT current_actor_is_admin())) AND current_actor_via() IS NULL);
CREATE POLICY oauth_grant_admin_bypass ON oauth_grant TO rotor_admin USING (true) WITH CHECK (true);

-- The consent writes the code it hands the browser, for a grant of the person consenting; the token endpoint alone
-- reads it back, as the admin role.
CREATE POLICY oauth_code_made_with_grant ON oauth_code FOR INSERT TO rotor_app
    WITH CHECK (EXISTS (SELECT 1 FROM oauth_grant g WHERE g.id = grant_id AND g.user_id = current_actor_id()));
CREATE POLICY oauth_code_admin_bypass ON oauth_code TO rotor_admin USING (true) WITH CHECK (true);
CREATE POLICY oauth_token_admin_bypass ON oauth_token TO rotor_admin USING (true) WITH CHECK (true);

REVOKE ALL ON assistant_setting, oauth_client, oauth_request, oauth_grant, oauth_code, oauth_token FROM rotor_app;
GRANT SELECT, UPDATE ON assistant_setting TO rotor_app;
GRANT SELECT (id, name, uri) ON oauth_client TO rotor_app;
GRANT SELECT, INSERT, DELETE ON oauth_grant TO rotor_app;
GRANT INSERT ON oauth_code TO rotor_app;
-- The default privileges grant both roles the function; nobody else gets it.
REVOKE ALL ON FUNCTION oauth_request_take(uuid) FROM PUBLIC;

-- +goose Down
DROP TRIGGER IF EXISTS audit_log_via ON audit_log;
ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_via_whole, DROP COLUMN IF EXISTS via_grant,
    DROP COLUMN IF EXISTS via_client;
DROP TRIGGER IF EXISTS approval_decided_in_person ON approval;
DROP TRIGGER IF EXISTS app_user_ends_grants ON app_user;
-- The function answers the request's rows, so it goes before their table.
DROP FUNCTION IF EXISTS oauth_request_take(uuid);
DROP TABLE IF EXISTS oauth_token, oauth_code, oauth_grant, oauth_request, oauth_client, assistant_setting;
DROP FUNCTION IF EXISTS oauth_grant_fixed(), oauth_grant_ended(), app_user_ends_grants(), approval_decided_in_person(),
    audit_log_via(), current_actor_via();
