-- Composite index for hashed/sharded tables to accelerate feed queries
CREATE INDEX IF NOT EXISTS idx_posts_hash_user_created ON posts_hash (user_id, created_at DESC);


