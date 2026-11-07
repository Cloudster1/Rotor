-- +goose Up
-- Long-lived keys a service sends spans to the OTLP endpoint with (#258), each sending as one account.
-- docs/api-contract-m18.md says what each column means; the numbers below are internal/config/ingest_keys.go's, held
-- to it by a test. Only the SHA-256 of a key is kept, never the key.
CREATE TABLE ingest_key (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    name         text NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 64),
    account_id   uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    secret_hash  bytea NOT NULL CHECK (octet_length(secret_hash) = 32),
    -- The key's last characters, so an administrator tells keys apart without the key.
    hint         text NOT NULL CHECK (char_length(hint) = 4),
    expires_at   timestamptz,
    last_used_at timestamptz,
    created_by   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    rotated_at   timestamptz,
    CONSTRAINT ingest_key_expires_after_made CHECK (expires_at IS NULL OR expires_at > created_at)
);
CREATE UNIQUE INDEX ingest_key_secret_unique ON ingest_key (secret_hash);
CREATE UNIQUE INDEX ingest_key_name_unique ON ingest_key (lower(name));
CREATE INDEX ingest_key_account ON ingest_key (account_id);

-- Each key is a standing credential to review, so a fifty-first is refused whoever inserts it.
-- +goose StatementBegin
CREATE FUNCTION ingest_key_cap() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    -- One insert at a time, so two administrators adding together cannot pass the cap between them.
    PERFORM pg_advisory_xact_lock(hashtext('ingest_key_cap'));
    IF (SELECT count(*) FROM ingest_key) >= 50 THEN
        RAISE EXCEPTION 'at most 50 ingest keys'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'ingest_key_cap';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER ingest_key_cap BEFORE INSERT ON ingest_key FOR EACH ROW EXECUTE FUNCTION ingest_key_cap();

ALTER TABLE ingest_key ENABLE ROW LEVEL SECURITY;
ALTER TABLE ingest_key FORCE  ROW LEVEL SECURITY;
CREATE POLICY ingest_key_admin ON ingest_key TO rotor_app
    USING ((SELECT current_actor_is_admin())) WITH CHECK ((SELECT current_actor_is_admin()));
-- A key is let in before anyone is signed in, and its use is noted then.
CREATE POLICY ingest_key_admin_bypass ON ingest_key TO rotor_admin USING (true) WITH CHECK (true);

-- +goose Down
DROP TABLE IF EXISTS ingest_key;
DROP FUNCTION IF EXISTS ingest_key_cap();
