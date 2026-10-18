-- +goose Up
-- The language model providers workflows may use (#20). A provider's key lives in OpenBao, never here.
CREATE TABLE provider (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    name          text NOT NULL UNIQUE CHECK (btrim(name) <> ''),
    kind          text NOT NULL CHECK (kind IN ('anthropic', 'openai', 'azure_openai', 'openai_compatible')),
    base_url      text NOT NULL DEFAULT '',
    api_version   text NOT NULL DEFAULT '',
    models        text[] NOT NULL DEFAULT '{}',
    default_model text NOT NULL DEFAULT '',
    is_default    boolean NOT NULL DEFAULT false,
    enabled       boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT provider_default_listed CHECK (default_model = '' OR default_model = ANY (models)),
    CONSTRAINT provider_default_named CHECK (NOT is_default OR default_model <> '')
);

-- One provider holds the installation's default model.
CREATE UNIQUE INDEX provider_one_default ON provider (is_default) WHERE is_default;
CREATE TRIGGER provider_updated_at BEFORE UPDATE ON provider FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE provider ENABLE ROW LEVEL SECURITY;
ALTER TABLE provider FORCE  ROW LEVEL SECURITY;

-- Everyone signed in reads the allowed models; administrators alone add, change or remove a provider.
CREATE POLICY provider_read ON provider FOR SELECT TO rotor_app USING (current_actor_id() IS NOT NULL);
CREATE POLICY provider_admin_add ON provider FOR INSERT TO rotor_app WITH CHECK ((SELECT current_actor_is_admin()));
CREATE POLICY provider_admin_change ON provider FOR UPDATE TO rotor_app
    USING ((SELECT current_actor_is_admin())) WITH CHECK ((SELECT current_actor_is_admin()));
CREATE POLICY provider_admin_remove ON provider FOR DELETE TO rotor_app USING ((SELECT current_actor_is_admin()));
CREATE POLICY provider_admin_bypass ON provider TO rotor_admin USING (true) WITH CHECK (true);

-- +goose Down
DROP TABLE IF EXISTS provider;
