-- +goose Up
-- The links a span names (#262): the spans it relates to, each with its attributes without text, at most 16 as
-- internal/config/traces.go's MaxSpanLinks says, held to it by a test. A column rather than events, since an event
-- happened at a time and a link has none. The cap's trigger counts their bytes too.
ALTER TABLE trace_span ADD COLUMN links jsonb NOT NULL DEFAULT '[]'::jsonb
    CHECK (jsonb_typeof(links) = 'array' AND jsonb_array_length(links) <= 16);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trace_span_cap() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    t trace%ROWTYPE;
BEGIN
    NEW.bytes := octet_length(NEW.name) + octet_length(NEW.status_message) + octet_length(NEW.attributes::text)
        + octet_length(NEW.events::text) + octet_length(NEW.links::text) + coalesce(octet_length(NEW.sealed_content), 0);
    NEW.received_at := now();
    -- One span at a time per trace, so the counts hold and a span id is taken once.
    SELECT * INTO t FROM trace WHERE id = NEW.trace FOR UPDATE;
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;
    IF EXISTS (SELECT 1 FROM trace_span s WHERE s.trace = NEW.trace AND s.span_id = NEW.span_id) THEN
        RETURN NULL;
    END IF;
    IF t.spans >= 10000 OR t.bytes + NEW.bytes > 33554432
        OR (t.owner_id IS NOT NULL
            AND (SELECT coalesce(sum(o.spans), 0) FROM trace o WHERE o.owner_id = t.owner_id) >= 100000) THEN
        UPDATE trace SET dropped = dropped + 1, cut_at = coalesce(cut_at, now()) WHERE id = t.id;
        RETURN NULL;
    END IF;
    UPDATE trace SET spans = spans + 1, bytes = bytes + NEW.bytes, last_span_at = now(), expired_at = NULL,
        content = content OR NEW.sealed_content IS NOT NULL
        WHERE id = t.id;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trace_span_cap() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    t trace%ROWTYPE;
BEGIN
    NEW.bytes := octet_length(NEW.name) + octet_length(NEW.status_message) + octet_length(NEW.attributes::text)
        + octet_length(NEW.events::text) + coalesce(octet_length(NEW.sealed_content), 0);
    NEW.received_at := now();
    -- One span at a time per trace, so the counts hold and a span id is taken once.
    SELECT * INTO t FROM trace WHERE id = NEW.trace FOR UPDATE;
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;
    IF EXISTS (SELECT 1 FROM trace_span s WHERE s.trace = NEW.trace AND s.span_id = NEW.span_id) THEN
        RETURN NULL;
    END IF;
    IF t.spans >= 10000 OR t.bytes + NEW.bytes > 33554432
        OR (t.owner_id IS NOT NULL
            AND (SELECT coalesce(sum(o.spans), 0) FROM trace o WHERE o.owner_id = t.owner_id) >= 100000) THEN
        UPDATE trace SET dropped = dropped + 1, cut_at = coalesce(cut_at, now()) WHERE id = t.id;
        RETURN NULL;
    END IF;
    UPDATE trace SET spans = spans + 1, bytes = bytes + NEW.bytes, last_span_at = now(), expired_at = NULL,
        content = content OR NEW.sealed_content IS NOT NULL
        WHERE id = t.id;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
ALTER TABLE trace_span DROP COLUMN IF EXISTS links;
