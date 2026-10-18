-- +goose Up
-- Sign-in through the suite's identity provider (#6). Only rotor_admin touches these tables: the app role
-- holds no privilege on identities, sign-in attempts or sessions, so no request can read another's.

-- The provider's stable name for a person, which survives an email change.
CREATE TABLE user_identity (
    issuer     text NOT NULL,
    subject    text NOT NULL,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (issuer, subject)
);
CREATE INDEX user_identity_user_idx ON user_identity (user_id);

-- A sign-in in flight, good for one callback. The state is kept as its SHA-256.
CREATE TABLE oidc_login (
    state_hash    bytea PRIMARY KEY CHECK (length(state_hash) = 32),
    nonce         text NOT NULL,
    code_verifier text NOT NULL,
    next          text NOT NULL,
    expires_at    timestamptz NOT NULL
);

-- A signed-in browser. The cookie carries the token; the database keeps only its SHA-256.
CREATE TABLE user_session (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id      uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    token_hash   bytea NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    oidc_sid     text,
    user_agent   text,
    ip           inet,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);
CREATE INDEX user_session_user_idx ON user_session (user_id);
CREATE INDEX user_session_sid_idx ON user_session (oidc_sid) WHERE oidc_sid IS NOT NULL;

-- Logout tokens already acted on, so a replayed one is refused until it would have expired anyway.
CREATE TABLE oidc_logout_jti (
    jti        text PRIMARY KEY,
    expires_at timestamptz NOT NULL
);

ALTER TABLE user_identity   ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_identity   FORCE  ROW LEVEL SECURITY;
ALTER TABLE oidc_login      ENABLE ROW LEVEL SECURITY;
ALTER TABLE oidc_login      FORCE  ROW LEVEL SECURITY;
ALTER TABLE user_session    ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_session    FORCE  ROW LEVEL SECURITY;
ALTER TABLE oidc_logout_jti ENABLE ROW LEVEL SECURITY;
ALTER TABLE oidc_logout_jti FORCE  ROW LEVEL SECURITY;

CREATE POLICY user_identity_admin_bypass ON user_identity TO rotor_admin USING (true) WITH CHECK (true);
CREATE POLICY oidc_login_admin_bypass ON oidc_login TO rotor_admin USING (true) WITH CHECK (true);
CREATE POLICY user_session_admin_bypass ON user_session TO rotor_admin USING (true) WITH CHECK (true);
CREATE POLICY oidc_logout_jti_admin_bypass ON oidc_logout_jti TO rotor_admin USING (true) WITH CHECK (true);

REVOKE ALL ON user_identity, oidc_login, user_session, oidc_logout_jti FROM rotor_app;

-- +goose Down
DROP TABLE IF EXISTS oidc_logout_jti, user_session, oidc_login, user_identity;
