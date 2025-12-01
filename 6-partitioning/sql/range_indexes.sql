-- Create composite index on (user_id, created_at DESC) for each monthly partition.
-- Adjust months/years to match your partition set in range_schema.sql.

CREATE INDEX IF NOT EXISTS idx_posts_range_2024_01_user_created ON posts_range_2024_01 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2024_02_user_created ON posts_range_2024_02 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2024_03_user_created ON posts_range_2024_03 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2024_04_user_created ON posts_range_2024_04 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2024_05_user_created ON posts_range_2024_05 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2024_06_user_created ON posts_range_2024_06 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2024_07_user_created ON posts_range_2024_07 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2024_08_user_created ON posts_range_2024_08 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2024_09_user_created ON posts_range_2024_09 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2024_10_user_created ON posts_range_2024_10 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2024_11_user_created ON posts_range_2024_11 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2024_12_user_created ON posts_range_2024_12 (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_posts_range_2025_01_user_created ON posts_range_2025_01 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2025_02_user_created ON posts_range_2025_02 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2025_03_user_created ON posts_range_2025_03 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2025_04_user_created ON posts_range_2025_04 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2025_05_user_created ON posts_range_2025_05 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2025_06_user_created ON posts_range_2025_06 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2025_07_user_created ON posts_range_2025_07 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2025_08_user_created ON posts_range_2025_08 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2025_09_user_created ON posts_range_2025_09 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2025_10_user_created ON posts_range_2025_10 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2025_11_user_created ON posts_range_2025_11 (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_range_2025_12_user_created ON posts_range_2025_12 (user_id, created_at DESC);


