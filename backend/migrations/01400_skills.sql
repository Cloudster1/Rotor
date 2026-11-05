-- +goose Up
-- The skill library (#219): skills in the Agent Skills format, kept as immutable, numbered versions of their files,
-- as workflows keep versions of their YAML. docs/api-contract-m17.md agrees the tables; 01410_skill_access.sql says
-- who reads and writes them.

CREATE TABLE skill (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    -- Workflows name skills by it, so one name means one skill in the whole installation.
    name            text NOT NULL CONSTRAINT skill_name_unique UNIQUE
        CONSTRAINT skill_name_rule CHECK (char_length(name) <= 64 AND name ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    -- The current version's, so a listing reads one row per skill.
    description     text NOT NULL DEFAULT '',
    owner_id        uuid REFERENCES app_user(id) ON DELETE RESTRICT,
    -- The owner's name as of the first save: a person sees only their own row of app_user.
    owner_name      text NOT NULL DEFAULT '',
    current_version integer NOT NULL DEFAULT 0 CHECK (current_version >= 0),
    archived_at     timestamptz,
    -- A built-in skill ships with Rotor and is written by the api's start alone (#225).
    builtin         boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT skill_owned CHECK (builtin OR owner_id IS NOT NULL)
);
CREATE INDEX skill_owner_idx ON skill (owner_id) WHERE owner_id IS NOT NULL;
CREATE TRIGGER skill_updated_at BEFORE UPDATE ON skill FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE skill_version (
    skill_id        uuid NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    number          integer NOT NULL CHECK (number > 0),
    front_matter    jsonb NOT NULL,
    description     text NOT NULL CHECK (char_length(description) BETWEEN 1 AND 1024),
    -- sha256 over the sorted paths and contents, so the same files hash the same whoever saves them.
    hash            text NOT NULL,
    bytes           integer NOT NULL CHECK (bytes >= 0),
    comment         text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 500),
    created_by      uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_by_name text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (skill_id, number)
);

CREATE TABLE skill_file (
    skill_id uuid NOT NULL,
    number   integer NOT NULL,
    path     text NOT NULL,
    content  text NOT NULL,
    bytes    integer NOT NULL,
    sha256   text NOT NULL,
    PRIMARY KEY (skill_id, number, path),
    FOREIGN KEY (skill_id, number) REFERENCES skill_version(skill_id, number) ON DELETE CASCADE,
    -- What a listing shows of a file is what the file is, whoever wrote the row.
    CONSTRAINT skill_file_measured CHECK (bytes = octet_length(content)
        AND sha256 = encode(sha256(convert_to(content, 'UTF8')), 'hex'))
);

-- Versions run 1, 2, 3 as a workflow's do, each the one after the current, which it then becomes along with its
-- description. A built-in skill takes the number its metadata.version declares, so it only has to be above (#225).
-- +goose StatementBegin
CREATE FUNCTION skill_version_next() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    current integer;
    shipped boolean;
BEGIN
    SELECT current_version, builtin INTO current, shipped FROM skill WHERE id = NEW.skill_id FOR UPDATE;
    IF current IS NULL OR (shipped AND NEW.number <= current) OR (NOT shipped AND NEW.number <> current + 1) THEN
        RAISE EXCEPTION 'version % of a skill does not follow its current version %', NEW.number, current
            USING ERRCODE = 'check_violation', CONSTRAINT = 'skill_version_in_sequence';
    END IF;
    UPDATE skill SET current_version = NEW.number, description = NEW.description WHERE id = NEW.skill_id;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER skill_version_in_sequence BEFORE INSERT ON skill_version
    FOR EACH ROW EXECUTE FUNCTION skill_version_next();

-- Workflows pin a skill by its name, and the owner is who it was made by: neither moves, and nor does being built in.
-- +goose StatementBegin
CREATE FUNCTION skill_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.name <> OLD.name OR NEW.owner_id IS DISTINCT FROM OLD.owner_id OR NEW.builtin <> OLD.builtin THEN
        RAISE EXCEPTION 'a skill keeps its name, its owner and whether it is built in'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'skill_fixed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER skill_fixed BEFORE UPDATE ON skill FOR EACH ROW EXECUTE FUNCTION skill_fixed();

-- Nobody reads or writes a skill through rotor_app until 01410_skill_access.sql adds the policies.
ALTER TABLE skill ENABLE ROW LEVEL SECURITY;
ALTER TABLE skill FORCE  ROW LEVEL SECURITY;
ALTER TABLE skill_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE skill_version FORCE  ROW LEVEL SECURITY;
ALTER TABLE skill_file ENABLE ROW LEVEL SECURITY;
ALTER TABLE skill_file FORCE  ROW LEVEL SECURITY;
CREATE POLICY skill_admin_bypass ON skill TO rotor_admin USING (true) WITH CHECK (true);
CREATE POLICY skill_version_admin_bypass ON skill_version TO rotor_admin USING (true) WITH CHECK (true);
CREATE POLICY skill_file_admin_bypass ON skill_file TO rotor_admin USING (true) WITH CHECK (true);
-- Skills are archived, never deleted, and a version never changes: a workflow pinning one keeps meaning what it meant.
REVOKE DELETE ON skill FROM rotor_app;
REVOKE UPDATE, DELETE ON skill_version FROM rotor_app, rotor_admin;
REVOKE UPDATE, DELETE ON skill_file FROM rotor_app, rotor_admin;

-- +goose Down
DROP TABLE IF EXISTS skill_file;
DROP TABLE IF EXISTS skill_version;
DROP TABLE IF EXISTS skill;
DROP FUNCTION IF EXISTS skill_fixed();
DROP FUNCTION IF EXISTS skill_version_next();
