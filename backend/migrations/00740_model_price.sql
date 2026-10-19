-- +goose Up
-- What a model of a provider costs (#38), in US dollars per million tokens read and written; a model without a
-- row counts tokens only, and a workflow cannot bound its cost.
CREATE TABLE model_price (
    provider_id        uuid NOT NULL REFERENCES provider(id) ON DELETE CASCADE,
    model              text NOT NULL CHECK (btrim(model) <> ''),
    input_per_million  numeric(12, 4) NOT NULL CHECK (input_per_million >= 0),
    output_per_million numeric(12, 4) NOT NULL CHECK (output_per_million >= 0),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_id, model)
);

CREATE TRIGGER model_price_updated_at BEFORE UPDATE ON model_price FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE model_price ENABLE ROW LEVEL SECURITY;
ALTER TABLE model_price FORCE  ROW LEVEL SECURITY;

-- Everyone signed in reads the prices, as they read the models; administrators alone set them.
CREATE POLICY model_price_read ON model_price FOR SELECT TO rotor_app USING (current_actor_id() IS NOT NULL);
CREATE POLICY model_price_admin_add ON model_price FOR INSERT TO rotor_app WITH CHECK ((SELECT current_actor_is_admin()));
CREATE POLICY model_price_admin_change ON model_price FOR UPDATE TO rotor_app
    USING ((SELECT current_actor_is_admin())) WITH CHECK ((SELECT current_actor_is_admin()));
CREATE POLICY model_price_admin_remove ON model_price FOR DELETE TO rotor_app USING ((SELECT current_actor_is_admin()));
CREATE POLICY model_price_admin_bypass ON model_price TO rotor_admin USING (true) WITH CHECK (true);

-- +goose Down
DROP TABLE IF EXISTS model_price;
