-- +goose Up
-- A person's connections to the tools. The refresh token behind each lives in OpenBao, never here.
CREATE TABLE connection (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id           uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    tool_id           uuid NOT NULL REFERENCES tool(id) ON DELETE CASCADE,
    scopes            text[] NOT NULL DEFAULT '{}',
    status            text NOT NULL DEFAULT 'connected' CHECK (status IN ('connected', 'reconnect')),
    connected_at      timestamptz NOT NULL DEFAULT now(),
    last_used_at      timestamptz,
    last_refreshed_at timestamptz,
    UNIQUE (user_id, tool_id)
);

CREATE INDEX connection_tool_idx ON connection (tool_id);

ALTER TABLE connection ENABLE ROW LEVEL SECURITY;
ALTER TABLE connection FORCE  ROW LEVEL SECURITY;

-- A person sees and ends their own connections; the control plane makes and refreshes them as rotor_admin.
CREATE POLICY connection_own_read ON connection FOR SELECT TO rotor_app USING (user_id = current_actor_id());
CREATE POLICY connection_own_end ON connection FOR DELETE TO rotor_app USING (user_id = current_actor_id());
CREATE POLICY connection_admin_bypass ON connection TO rotor_admin USING (true) WITH CHECK (true);
REVOKE INSERT, UPDATE ON connection FROM rotor_app;

-- A connection started and not yet consented to: single use, bound to the session that started it.
CREATE TABLE connection_login (
    state_hash    bytea PRIMARY KEY CHECK (length(state_hash) = 32),
    session_id    uuid NOT NULL REFERENCES user_session(id) ON DELETE CASCADE,
    user_id       uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    tool_id       uuid NOT NULL REFERENCES tool(id) ON DELETE CASCADE,
    code_verifier text NOT NULL,
    expires_at    timestamptz NOT NULL
);

CREATE INDEX connection_login_expires_idx ON connection_login (expires_at);

ALTER TABLE connection_login ENABLE ROW LEVEL SECURITY;
ALTER TABLE connection_login FORCE  ROW LEVEL SECURITY;
CREATE POLICY connection_login_admin_bypass ON connection_login TO rotor_admin USING (true) WITH CHECK (true);
REVOKE ALL ON connection_login FROM rotor_app;

-- +goose Down
DROP TABLE IF EXISTS connection_login;
DROP TABLE IF EXISTS connection;
