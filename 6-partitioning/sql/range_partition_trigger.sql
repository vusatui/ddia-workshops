-- Trigger-based auto-creation of monthly partitions for posts_range using a DEFAULT partition.
-- Parent is assumed: posts_range PARTITION BY RANGE (created_at).
-- Flow:
-- 1) Create DEFAULT partition if missing
-- 2) BEFORE INSERT trigger on the DEFAULT partition:
--    - create the proper month partition if needed
--    - re-insert the row into the parent (which now routes to the correct child)
--    - return NULL to skip insert into DEFAULT

CREATE TABLE IF NOT EXISTS posts_range_default PARTITION OF posts_range DEFAULT;

CREATE OR REPLACE FUNCTION ensure_month_partition(p_parent regclass, p_ts timestamp)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    start_date date := date_trunc('month', p_ts)::date;
    end_date   date := (date_trunc('month', p_ts) + interval '1 month')::date;
    part_name  text := format('%s_%s', p_parent::text, to_char(start_date, 'YYYY_MM'));
    exists     bool;
BEGIN
    SELECT to_regclass(part_name) IS NOT NULL INTO exists;
    IF NOT exists THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF %s FOR VALUES FROM (%L) TO (%L);',
            part_name, p_parent::text, start_date, end_date
        );
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION posts_range_default_redirect()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM ensure_month_partition('posts_range'::regclass, NEW.created_at);
    INSERT INTO posts_range (user_id, created_at, content)
    VALUES (NEW.user_id, NEW.created_at, NEW.content);
    RETURN NULL; -- skip original insert into DEFAULT
END;
$$;

DROP TRIGGER IF EXISTS trig_posts_range_default_redirect ON posts_range_default;
CREATE TRIGGER trig_posts_range_default_redirect
BEFORE INSERT ON posts_range_default
FOR EACH ROW
EXECUTE FUNCTION posts_range_default_redirect();




