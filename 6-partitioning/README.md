## DDIA Partitioning Workshop (Ready)

This directory contains a fully working solution demonstrating:
- Baseline (no partitioning)
- Range partitioning by month
- Hash partitioning (sharding by user_id) and routing layer

Everything runs locally via Docker Compose. The `app` container runs Go code via `go run`.

### Services
- postgres_baseline: holds baseline table and range-partitioned tables (ports: 5432)
- postgres_shard_1: shard 1 (ports: 5433->5432)
- postgres_shard_2: shard 2 (ports: 5434->5432)
- postgres_shard_3: shard 3 (ports: 5435->5432)
- app: Go environment (idle: `sleep infinity`)

All Postgres instances use password `postgres`.

---

### 1) Start infrastructure

```bash
docker-compose up --build -d
```

Wait until all services are healthy.

---

### Walkthrough: from a plain table to partitioning

Below is a step-by-step walkthrough:
- how a plain table behaves without partitions;
- why partitioning helps on a real monthly window query;
- a monthly range partitioning example and execution plan comparison vs baseline;
- a separate app-level hash sharding demo (fan‑out/fan‑in).

---

### 2) Baseline schema (no partitions)

```bash
docker exec -i postgres_baseline psql -U postgres -d postgres < sql/baseline_schema.sql
docker exec -i postgres_baseline psql -U postgres -d postgres < sql/baseline_indexes.sql
docker exec -it postgres_baseline psql -U postgres -d postgres -c "ANALYZE posts;"
```

This creates a plain `posts` table with no partitioning. It is our baseline: all data and indexes are in a single object, which increases read/write cost and buffer usage as the table grows.

---

### 3) Seed baseline data

Defaults: users=10000, posts=1,000,000, batch=1000

```bash
docker exec -it app go run ./cmd/seed -mode=baseline -users=10000 -posts=1000000 -batch=1000
```

You can lower these values for a quicker run.

---

### 4) Baseline plan (EXPLAIN)

```bash
docker exec -it postgres_baseline psql -U postgres -d postgres -c "
EXPLAIN (ANALYZE, BUFFERS, VERBOSE)
SELECT id, user_id, created_at, content
FROM posts
WHERE user_id = ANY(ARRAY[1,2,3,4,5,6,7,8,9,10])
  AND created_at >= date_trunc('month', now())
  AND created_at <  date_trunc('month', now()) + interval '1 month'
ORDER BY created_at DESC
LIMIT 50;"
```

This is the plan for a monthly window on the non‑partitioned table. As data grows, the planner considers and reads larger portions of the index/table; buffer usage and latency go up.

---

### 5) Range partitioning by month

We use a dedicated parent `posts_range` to avoid clashing with the baseline `posts` table.

```bash
docker exec -i postgres_baseline psql -U postgres -d postgres < sql/range_schema.sql
```

After applying, copy the baseline data into the partitioned parent:

```bash
docker exec -it postgres_baseline psql -U postgres -d postgres -c "INSERT INTO posts_range (id, user_id, created_at, content) SELECT id, user_id, created_at, content FROM posts;"
```

Apply per‑partition indexes and analyze:

```bash
docker exec -i postgres_baseline psql -U postgres -d postgres < sql/range_indexes.sql
docker exec -it postgres_baseline psql -U postgres -d postgres -c "ANALYZE posts_range;"
```

---

### 6) Range plan (EXPLAIN) and comparison with baseline

```bash
docker exec -it postgres_baseline psql -U postgres -d postgres -c "
EXPLAIN (ANALYZE, BUFFERS, VERBOSE)
SELECT id, user_id, created_at, content
FROM posts_range
WHERE user_id = ANY(ARRAY[1,2,3,4,5,6,7,8,9,10])
  AND created_at >= date_trunc('month', now())
  AND created_at <  date_trunc('month', now()) + interval '1 month'
ORDER BY created_at DESC
LIMIT 50;"
```

This is the same query against the monthly‑partitioned parent. Look for:
- A sign of pruning (unneeded months dropped);
- only the target child `posts_range_YYYY_MM` being scanned (Index/Bitmap Scan);
- fewer pages/buffers and lower runtime vs the baseline.

To make the pruning effect explicit, run the same plan with pruning disabled, then enabled:

```bash
# Pruning OFF (expects many children considered, slower and more buffers)
docker exec -it postgres_baseline psql -U postgres -d postgres -c "
BEGIN;
SET LOCAL enable_partition_pruning = off;
EXPLAIN (ANALYZE, BUFFERS, VERBOSE)
SELECT id, user_id, created_at, content
FROM posts_range
WHERE user_id = ANY(ARRAY[1,2,3,4,5,6,7,8,9,10])
  AND created_at >= date_trunc('month', now())
  AND created_at <  date_trunc('month', now()) + interval '1 month'
ORDER BY created_at DESC
LIMIT 50;
ROLLBACK;"

# Pruning ON (default; expects “Subplans Removed: N” and one child touched)
docker exec -it postgres_baseline psql -U postgres -d postgres -c "
EXPLAIN (ANALYZE, BUFFERS, VERBOSE)
SELECT id, user_id, created_at, content
FROM posts_range
WHERE user_id = ANY(ARRAY[1,2,3,4,5,6,7,8,9,10])
  AND created_at >= date_trunc('month', now())
  AND created_at <  date_trunc('month', now()) + interval '1 month'
ORDER BY created_at DESC
LIMIT 50;"
```

---

### 7) App‑level hash sharding (separate demo)

```bash
docker exec -i postgres_shard_1 psql -U postgres -d postgres < sql/hash_schema.sql
docker exec -i postgres_shard_2 psql -U postgres -d postgres < sql/hash_schema.sql
docker exec -i postgres_shard_3 psql -U postgres -d postgres < sql/hash_schema.sql
docker exec -i postgres_shard_1 psql -U postgres -d postgres < sql/hash_indexes.sql
docker exec -i postgres_shard_2 psql -U postgres -d postgres < sql/hash_indexes.sql
docker exec -i postgres_shard_3 psql -U postgres -d postgres < sql/hash_indexes.sql
docker exec -it postgres_shard_1 psql -U postgres -d postgres -c "ANALYZE posts_hash;"
docker exec -it postgres_shard_2 psql -U postgres -d postgres -c "ANALYZE posts_hash;"
docker exec -it postgres_shard_3 psql -U postgres -d postgres -c "ANALYZE posts_hash;"
```

---

### 8) Seed sharded (hash) data

```bash
docker exec -it app go run ./cmd/seed -mode=hash -users=10000 -posts=1000000 -batch=1000
```

Routing is done in the application: `shard = user_id % 3`.

---

### 9) Benchmark hash (sharded) reads — optional

```bash
docker exec -it app go run ./cmd/benchmark -mode=hash \
  -concurrency=100 -requests=3000 -subs=100
```

The router fans out to shards by `user_id`, merges results, and sorts globally by `created_at DESC` (fan‑out/fan‑in). This demonstrates horizontal scaling in the application layer and is not directly compared with EXPLAIN for baseline/range.

---

## Consistent hashing: implementation and migration demo

Modulo-based sharding (user_id % N) has a major limitation: when N changes (adding/removing shards), most keys move. Consistent hashing minimizes key movement.

### The idea behind the *Ring*

Consistent hashing maps a huge keyspace onto a circular “ring” of points. Each physical shard is represented by many virtual points (replicas) on that ring. To pick a shard for a key:
- Hash the key to a 64‑bit number.
- Find the first ring point clockwise whose hash is ≥ key’s hash (binary search).
- The owner of that point is the shard that serves the key.

Why this helps:
- When you add/remove a shard, only the keys that “fall between” the shard’s new/removed points move. The rest keep their owners. That’s why only a fraction of keys migrate, roughly 1/(old_shards+1) when adding a shard.
- Virtual nodes make the distribution smoother: more replicas per shard → better balance and less variance.

How our code works:
- `Ring.Build(shards)` creates `replicas` virtual points per shard; each point has a 64‑bit position computed from `(shardIndex, replicaIndex)` with a mixing function for uniform spread, then the points are sorted.
- `Ring.Owner(keyHash)` does a `sort.Search` to find the first point ≥ `keyHash`, with wrap‑around to the first point if needed.
- `HashUser(userID)` converts the `int64` to bytes and hashes it (FNV‑1a) to get a stable 64‑bit key for the ring.


### Demo: how many keys move when adding a shard (3 → 4):

```bash
docker exec -it app go run ./cmd/demo_consistent
```

This section is isolated from the modulo-based example. It uses its own table `posts_hash_ch` to avoid interference.

What it demonstrates:
- Consistent hashing router and “ring” with virtual nodes
- Seeding across 3 shards using ring(3)
- Adding a shard (using the baseline DB as shard #3)
- Migrating only the moved users’ rows
- Re-running the read benchmark and comparing stats

Run the demo end-to-end:

```bash
# The demo seeds data into shards 1..3, then adds shard #4 (baseline),
# migrates the moved users and runs before/after benchmarks.
docker exec -it app go run ./cmd/demo_consistent \
  -users=2000 -posts-per-user=3 -batch=500 \
  -requests=300 -concurrency=20 -limit=50
```

Notes:
- Tables used: `posts_hash_ch` on shards 1..3 and the baseline DB (as shard #4).
- Router used: `ConsistentHashRouter` with the table set to `posts_hash_ch`.
- You can adjust flags for larger runs; defaults are chosen to finish quickly.

---

## Partition management: missing partitions and auto-creation

Not all systems manage partitions automatically. With range partitioning by month, inserting a row into a non-existent month will fail by default.

Reproduce the error (insert into an unseen future month):

```bash
docker exec -it postgres_baseline psql -U postgres -d postgres -c \
  "INSERT INTO posts_range (user_id, created_at, content) VALUES (1, '2026-02-01', 'out of range');"
# Expected: ERROR: no partition of relation "posts_range" found for row
```

 Approach: pg_partman (scheduled partition management)
- Capabilities: pre-create future partitions, retention/archival, scheduled maintenance
- Setup in this project (Debian-based postgres image):
  ```bash
  docker exec -it postgres_baseline bash -lc "apt-get update && apt-get install -y postgresql-16-partman"
  # Set up an isolated demo parent (posts_partman) and pre-create future monthly partitions
  docker exec -i postgres_baseline psql -U postgres -d postgres < sql/pg_partman_setup.sql
  ```
- Pros:
  - Production-grade: proactive partition creation, retention management, fewer surprises
  - No DDL on the hot write path
- Cons:
  - Extra dependency and setup
  - Operational overhead and learning curve

After applying one of the approaches, re-run an insert:

```bash
# With pg_partman (plain INSERT works if the partition was pre-created).
# Note: we use the isolated demo table posts_partman here.
docker exec -it postgres_baseline psql -U postgres -d postgres -c \
  "INSERT INTO posts_partman (user_id, created_at, content) VALUES (3, '2026-03-01', 'ok with partman');"

# Verify partition exists:
docker exec -it postgres_baseline psql -U postgres -d postgres -c \
"SELECT * FROM partman.show_partitions('public.posts_partman');"

# Writing to non-excistig partition. By defaul writes to default table
docker exec -it postgres_baseline psql -U postgres -d postgres -c \
  "INSERT INTO posts_partman (user_id, created_at, content) VALUES (3, '2027-03-01', 'ok with partman');"

# Show created record
docker exec -it postgres_baseline psql -U postgres -d postgres -c \
  "SELECT * FROM public.posts_partman_default WHERE  created_at >= '2027-03-01'::timestamp AND  created_at <  '2027-04-01'::timestamp;"

# Partition data
docker exec -it postgres_baseline psql -U postgres -d postgres -c \
  "SELECT partman.partition_data_time('public.posts_partman');"  

# Show that table is empty
docker exec -it postgres_baseline psql -U postgres -d postgres -c \
  "SELECT * FROM public.posts_partman_default WHERE  created_at >= '2027-03-01'::timestamp AND  created_at <  '2027-04-01'::timestamp;"

# Verify new partition exists:
docker exec -it postgres_baseline psql -U postgres -d postgres -c \
"SELECT * FROM partman.show_partitions('public.posts_partman');"
```

---

## Join across partitions (execution plan demo)

In this step we show how the planner prunes partitions on both sides of a join when predicates are aligned with the partition key. We use the isolated pg_partman demo tables to avoid affecting other workshop steps.

1) Ensure pg_partman demo tables exist
```bash
# posts_partman (created earlier)
docker exec -i postgres_baseline psql -U postgres -d postgres < sql/pg_partman_setup.sql

# comments_partman (new)
docker exec -i postgres_baseline psql -U postgres -d postgres < sql/pg_partman_setup_comments.sql
```

2) Seed a few rows to have matching months
```bash
docker exec -it postgres_baseline psql -U postgres -d postgres -c \
  "INSERT INTO posts_partman (id, user_id, created_at, content) VALUES
   (100001, 1, '2026-03-05', 'p-mar'),
   (100002, 2, '2026-03-10', 'p-mar'),
   (100003, 2, '2026-04-02', 'p-apr');"

docker exec -it postgres_baseline psql -U postgres -d postgres -c \
  "INSERT INTO comments_partman (id, post_id, user_id, created_at, content) VALUES
   (200001, 100001, 11, '2026-03-06', 'c-mar'),
   (200002, 100001, 12, '2026-03-12', 'c-mar'),
   (200003, 100003, 12, '2026-04-05', 'c-apr');"
```

3) Run EXPLAIN (ANALYZE, BUFFERS) for a join with a monthly window
```bash
docker exec -it postgres_baseline psql -U postgres -d postgres -c "
EXPLAIN (ANALYZE, BUFFERS)
SELECT p.id, c.id, p.created_at AS p_ts, c.created_at AS c_ts
FROM   posts_partman p
JOIN   comments_partman c ON c.post_id = p.id
WHERE  p.created_at >= '2026-03-01' AND p.created_at < '2026-04-01'
  AND  c.created_at >= '2026-03-01' AND c.created_at < '2026-04-01'
ORDER  BY p.created_at DESC
LIMIT  10;"
```

What to look for:
- Partition Pruning on both parents (only the 2026‑03 children are scanned).
- Append/Bitmap/Index scans localized to the pruned children.
- A small row count and low buffer usage vs. scanning many children.

---

## Clean-up

```bash
docker-compose down -v
```
