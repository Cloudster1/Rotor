-- +goose Up
-- An assistant may be known by the address of its client ID metadata document rather than by registering (#114),
-- as docs/api-contract-m10.md agrees; the columns and rules are Exciter's 00800_oauth.sql at 3b340ca. A document
-- keeps no secret, is fetched again once stale, and is counted against no address, since nobody registered it.

ALTER TABLE oauth_client
    ADD COLUMN kind text NOT NULL DEFAULT 'registered' CHECK (kind IN ('registered', 'metadata')),
    -- Until when a document may be used without fetching it again; null for a registration.
    ADD COLUMN fresh_until timestamptz,
    ALTER COLUMN registered_from DROP NOT NULL,
    DROP CONSTRAINT oauth_client_id_check,
    ADD CONSTRAINT oauth_client_id_check CHECK (char_length(id) BETWEEN 1 AND 2048),
    ADD CONSTRAINT oauth_client_documents_keep_no_secret CHECK (kind = 'registered' OR auth_method = 'none'),
    ADD CONSTRAINT oauth_client_documents_are_fetched CHECK ((kind = 'metadata') = (fresh_until IS NOT NULL)),
    ADD CONSTRAINT oauth_client_registrations_have_address CHECK ((kind = 'registered') = (registered_from IS NOT NULL));

DROP INDEX oauth_client_registered_idx;
CREATE INDEX oauth_client_registered_idx ON oauth_client (registered_from, created_at) WHERE kind = 'registered';

-- The consent page and the lists say how Rotor knows each assistant.
GRANT SELECT (kind) ON oauth_client TO rotor_app;

-- +goose Down
REVOKE SELECT (kind) ON oauth_client FROM rotor_app;
-- A document's client has no address it registered from, so it cannot outlive the columns that describe it.
DELETE FROM oauth_client WHERE kind = 'metadata';
DROP INDEX oauth_client_registered_idx;
CREATE INDEX oauth_client_registered_idx ON oauth_client (registered_from, created_at);
ALTER TABLE oauth_client
    DROP CONSTRAINT oauth_client_registrations_have_address,
    DROP CONSTRAINT oauth_client_documents_are_fetched,
    DROP CONSTRAINT oauth_client_documents_keep_no_secret,
    DROP CONSTRAINT oauth_client_id_check,
    ADD CONSTRAINT oauth_client_id_check CHECK (char_length(id) BETWEEN 1 AND 200),
    ALTER COLUMN registered_from SET NOT NULL,
    DROP COLUMN fresh_until,
    DROP COLUMN kind;
