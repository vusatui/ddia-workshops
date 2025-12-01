-- Enable pg_partman and configure it to manage native monthly partitions for posts_range.
-- Prerequisites:
--   1) This script is ISOLATED: it creates a separate demo parent table (posts_partman)
--      so it does not interfere with the workshop's posts_range table.
--   2) Extension pg_partman is installed in the cluster
-- This script:
--   - Ensures extension exists
--   - Creates an isolated parent table posts_partman (declarative range partitioning)
--   - Registers it with pg_partman for monthly partitions
--   - Creates a few future partitions and runs maintenance

CREATE SCHEMA IF NOT EXISTS partman;
CREATE EXTENSION IF NOT EXISTS pg_partman WITH SCHEMA partman;

-- Create an isolated parent table for the pg_partman demo
-- This avoids modifying the main workshop table posts_range.
DO $$
BEGIN
  IF to_regclass('public.posts_partman') IS NULL THEN
    CREATE TABLE public.posts_partman (
      id BIGINT,
      user_id BIGINT,
      created_at TIMESTAMP NOT NULL,
      content TEXT
    ) PARTITION BY RANGE (created_at);
  ELSE
    -- Ensure NOT NULL on control column in case the table existed
    ALTER TABLE public.posts_partman ALTER COLUMN created_at SET NOT NULL;
  END IF;
END$$;

-- Docs https://github.com/pgpartman/pg_partman/blob/development/doc/pg_partman.md#creation-objects
SELECT partman.create_parent(
  p_parent_table          := 'public.posts_partman',
  p_control               := 'created_at',
  p_interval              := '1 month',
  p_premake               := 6,          -- create 6 future partitions
  p_default_table         := true,
  p_automatic_maintenance := 'on'
);


