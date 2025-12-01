-- Composite index for baseline table to accelerate feed queries
CREATE INDEX IF NOT EXISTS idx_posts_user_created ON posts (user_id, created_at DESC);


