-- +goose Up
-- A tool's MCP endpoint is no longer its resource: Armature names its whole API as the resource and serves MCP under
-- it (#45). grant_types are what its authorization server offers, token exchange among them, and a client a tool
-- holds pending until its administrator allows Rotor is awaiting_approval.
ALTER TABLE tool ADD COLUMN mcp_url text;
UPDATE tool SET mcp_url = resource;
ALTER TABLE tool ALTER COLUMN mcp_url SET NOT NULL,
    ADD COLUMN grant_types text[] NOT NULL DEFAULT '{}',
    DROP CONSTRAINT tool_status_check,
    ADD CONSTRAINT tool_status_check CHECK (status IN ('ready', 'needs_registration', 'awaiting_approval'));

-- +goose Down
UPDATE tool SET status = 'needs_registration' WHERE status = 'awaiting_approval';
ALTER TABLE tool DROP COLUMN mcp_url, DROP COLUMN grant_types,
    DROP CONSTRAINT tool_status_check,
    ADD CONSTRAINT tool_status_check CHECK (status IN ('ready', 'needs_registration'));
