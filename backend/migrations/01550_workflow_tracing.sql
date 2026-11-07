-- +goose Up
-- What a workflow chooses of its runs' traces (#259): whether they keep text, which destinations get them, and the
-- share of runs sent there. docs/api-contract-m18.md says what each column means; a workflow without a row has the
-- defaults. The cap on destinations is internal/config/traces.go's MaxTraceDestinations, held to it by a test.
CREATE TABLE workflow_tracing (
    workflow_id  uuid PRIMARY KEY REFERENCES workflow(id) ON DELETE CASCADE,
    -- Only less text than the installation keeps: an installation administrator's choice to keep none holds.
    content      text NOT NULL DEFAULT 'inherit' CHECK (content IN ('inherit', 'none')),
    -- NULL sends to every enabled destination, ones added later too; a list to those of them alone.
    destinations uuid[] CHECK (destinations IS NULL OR cardinality(destinations) <= 8),
    sample_ratio double precision NOT NULL DEFAULT 1 CHECK (sample_ratio >= 0 AND sample_ratio <= 1),
    changed_by   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER workflow_tracing_updated_at BEFORE UPDATE ON workflow_tracing FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE workflow_tracing ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_tracing FORCE  ROW LEVEL SECURITY;
CREATE POLICY workflow_tracing_read ON workflow_tracing FOR SELECT TO rotor_app USING (actor_can('workflow.read', workflow_id));
CREATE POLICY workflow_tracing_add ON workflow_tracing FOR INSERT TO rotor_app
    WITH CHECK (actor_can('workflow.administer', workflow_id));
CREATE POLICY workflow_tracing_change ON workflow_tracing FOR UPDATE TO rotor_app
    USING (actor_can('workflow.administer', workflow_id)) WITH CHECK (actor_can('workflow.administer', workflow_id));
CREATE POLICY workflow_tracing_remove ON workflow_tracing FOR DELETE TO rotor_app
    USING (actor_can('workflow.administer', workflow_id));
-- The api reads every workflow's choice as its runs' spans arrive, whoever sent them.
CREATE POLICY workflow_tracing_admin_bypass ON workflow_tracing TO rotor_admin USING (true) WITH CHECK (true);

-- +goose Down
DROP TABLE IF EXISTS workflow_tracing;
