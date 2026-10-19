-- +goose Up
-- The events each tool delivered (#35), by the id the tool gives them, so a delivery the tool retries or sends again
-- starts nothing twice; kept for config.HookEventRetention. Only the control plane reads or writes them.
CREATE TABLE hook_event (
    tool_id     uuid NOT NULL REFERENCES tool(id) ON DELETE CASCADE,
    event_id    text NOT NULL CHECK (event_id <> ''),
    topic       text NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tool_id, event_id)
);

CREATE INDEX hook_event_received_idx ON hook_event (received_at);

ALTER TABLE hook_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE hook_event FORCE  ROW LEVEL SECURITY;
CREATE POLICY hook_event_admin_bypass ON hook_event TO rotor_admin USING (true) WITH CHECK (true);
REVOKE ALL ON hook_event FROM rotor_app;

-- When an administrator gave Rotor the tool's webhook secret, which lives in OpenBao, and when a delivery last came.
ALTER TABLE tool ADD COLUMN hook_secret_set_at    timestamptz,
                 ADD COLUMN hook_last_received_at timestamptz;

-- +goose Down
ALTER TABLE tool DROP COLUMN IF EXISTS hook_last_received_at, DROP COLUMN IF EXISTS hook_secret_set_at;
DROP TABLE IF EXISTS hook_event;
