-- +goose Up
-- A tool's name is what workflows bind, and only an administrator changes it (#267); its product is what its /meta
-- names, which decides how Rotor asks it for tokens. Until now the two were one column, so each starts as the name.
ALTER TABLE tool ADD COLUMN product text;
UPDATE tool SET product = name;
ALTER TABLE tool ALTER COLUMN product SET NOT NULL;
ALTER TABLE tool ADD CONSTRAINT tool_product_named CHECK (btrim(product) <> '');
CREATE INDEX tool_name_lower ON tool (lower(name));

-- A unique index would not build over the names installations already share, and renaming those here would bind
-- their workflows to a tool nobody chose. So new duplicates are refused whoever writes them, and the old ones are
-- listed on Admin, Tools for an administrator to rename; until then a binding of such a name resolves to none.
-- +goose StatementBegin
CREATE FUNCTION tool_name_unique() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    -- One writer per name at a time, so two administrators adding the same name together cannot both pass.
    PERFORM pg_advisory_xact_lock(hashtext('tool_name:' || lower(NEW.name)));
    IF EXISTS (SELECT 1 FROM tool WHERE lower(name) = lower(NEW.name) AND id <> NEW.id) THEN
        RAISE EXCEPTION 'a tool called % exists already', NEW.name
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'tool_name_unique';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER tool_name_unique_add BEFORE INSERT ON tool FOR EACH ROW EXECUTE FUNCTION tool_name_unique();
-- Only a change of name is checked, so a tool that shares its name from before can still be checked and registered.
CREATE TRIGGER tool_name_unique_rename BEFORE UPDATE OF name ON tool FOR EACH ROW
    WHEN (lower(OLD.name) IS DISTINCT FROM lower(NEW.name)) EXECUTE FUNCTION tool_name_unique();

-- +goose Down
-- Names stay as administrators set them, since workflows bind them; the code before this takes its product from them.
DROP TRIGGER tool_name_unique_rename ON tool;
DROP TRIGGER tool_name_unique_add ON tool;
DROP FUNCTION tool_name_unique();
DROP INDEX tool_name_lower;
ALTER TABLE tool DROP COLUMN product;
