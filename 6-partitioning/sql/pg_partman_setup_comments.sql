-- Isolated demo: partitioned comments table managed by pg_partman.
-- Creates public.comments_partman and registers it for monthly partitions.
-- Requires: pg_partman extension installed in schema "partman".

CREATE SCHEMA IF NOT EXISTS partman;
CREATE EXTENSION IF NOT EXISTS pg_partman WITH SCHEMA partman;

DO $$
BEGIN
  IF to_regclass('public.comments_partman') IS NULL THEN
    CREATE TABLE public.comments_partman (
      id BIGINT,
      post_id BIGINT,
      user_id BIGINT,
      created_at TIMESTAMP NOT NULL,
      content TEXT
    ) PARTITION BY RANGE (created_at);
  ELSE
    ALTER TABLE public.comments_partman ALTER COLUMN created_at SET NOT NULL;
  END IF;
END$$;

-- Register comments_partman as monthly, declarative set and premake future partitions
SELECT partman.create_parent(
  p_parent_table          := 'public.comments_partman',
  p_control               := 'created_at',
  p_interval              := '1 month',
  p_premake               := 6,
  p_default_table         := true,
  p_automatic_maintenance := 'on'
);

-- Optionally run maintenance right away to precreate children
SELECT partman.run_maintenance();


