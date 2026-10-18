-- +goose Up
-- Which access tokens a run was handed, so each is revoked at its tool when the run ends (#23). The token itself
-- lives in OpenBao under the row's id until then, never here.
CREATE TABLE run_token (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    run_id     uuid NOT NULL REFERENCES run(id) ON DELETE CASCADE,
    tool_id    uuid NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX run_token_unrevoked_idx ON run_token (run_id) WHERE revoked_at IS NULL;

ALTER TABLE run_token ENABLE ROW LEVEL SECURITY;
ALTER TABLE run_token FORCE  ROW LEVEL SECURITY;
CREATE POLICY run_token_admin_bypass ON run_token TO rotor_admin USING (true) WITH CHECK (true);
REVOKE ALL ON run_token FROM rotor_app;

-- +goose Down
DROP TABLE IF EXISTS run_token;
