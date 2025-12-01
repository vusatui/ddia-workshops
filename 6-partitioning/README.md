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

### 2) Apply baseline schema

```bash
docker exec -i postgres_baseline psql -U postgres -d postgres < sql/baseline_schema.sql
docker exec -i postgres_baseline psql -U postgres -d postgres < sql/baseline_indexes.sql
docker exec -it postgres_baseline psql -U postgres -d postgres -c "ANALYZE posts;"
```

This creates table `posts` (no partitioning).

---

### 3) Seed baseline data

Defaults: users=10000, posts=1,000,000, batch=1000

```bash
docker exec -it app go run ./cmd/seed -mode=baseline -users=10000 -posts=1000000 -batch=1000
```

You can tweak parameters as needed (e.g., fewer posts for quick runs).

---

### 4) Benchmark baseline

```bash
docker exec -it app go run ./cmd/benchmark -mode=baseline \
  -concurrency=100 -requests=3000 -subs=100 -windowDays=7
```

Outputs: average latency, p95 latency, and total QPS.

---

### 5) Apply range partitioning schema

In the ready version, we use a dedicated partitioned table name `posts_range` to avoid clashing with the baseline `posts`.

```bash
docker exec -i postgres_baseline psql -U postgres -d postgres < sql/range_schema.sql
```

After applying, copy baseline data into partitioned table:

```bash
docker exec -it postgres_baseline psql -U postgres -d postgres -c "INSERT INTO posts_range (id, user_id, created_at, content) SELECT id, user_id, created_at, content FROM posts;"
```

Apply per-partition indexes and analyze:

```bash
docker exec -i postgres_baseline psql -U postgres -d postgres < sql/range_indexes.sql
docker exec -it postgres_baseline psql -U postgres -d postgres -c "ANALYZE posts_range;"
```

---

### 6) Benchmark range

```bash
docker exec -it app go run ./cmd/benchmark -mode=range \
  -concurrency=100 -requests=3000 -subs=100 -windowDays=7
```

This uses the routing that targets `posts_range` (partitioned by month).

---

### 7) Apply hash (shard) schema on each shard

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

### 8) Seed hash (sharded) data

```bash
docker exec -it app go run ./cmd/seed -mode=hash -users=10000 -posts=1000000 -batch=1000
```

Data is routed to shards using: `shard = user_id % 3`.

---

### 9) Benchmark hash (sharded)

```bash
docker exec -it app go run ./cmd/benchmark -mode=hash \
  -concurrency=100 -requests=3000 -subs=100 -windowDays=7
```

Router fans out to shards based on user IDs, merges results, and sorts globally by `created_at DESC`.

### 10) Compare results (baseline/range/hash)

Fill in after your runs:

| Method   | Avg latency | P95 | QPS |
|----------|-------------|-----|-----|
| Baseline | X           | X   | X   |
| Range    | X           | X   | X   |
| Hash     | X           | X   | X   |

You should observe improved performance from baseline -> range -> hash.

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

Two approaches to fix this:

1) Trigger-based auto-creation
- Install trigger (it will auto-create missing month partitions on INSERT):
  ```bash
  docker exec -i postgres_baseline psql -U postgres -d postgres < sql/range_partition_trigger.sql
  ```
- Test again (now plain INSERT succeeds):
  ```bash
  docker exec -it postgres_baseline psql -U postgres -d postgres -c \
    "INSERT INTO posts_range (user_id, created_at, content) VALUES (1, '2026-02-01', 'ok with trigger');"
  ```
- Verify partition:
  ```bash
  docker exec -it postgres_baseline psql -U postgres -d postgres -c "\dt posts_range_2026_02"
  ```
- Pros:
  - No external dependencies; fully controlled in SQL
  - Works transparently for any INSERT to the parent
- Cons:
  - DDL on the hot write path (partition creation under concurrent load needs care)
  - Does not handle retention/archival or proactive creation

Now remove the trigger and show the error again (using another future month):
```bash
docker exec -it postgres_baseline psql -U postgres -d postgres -c "DROP TRIGGER IF EXISTS trig_posts_range_default_redirect ON posts_range_default;"
docker exec -it postgres_baseline psql -U postgres -d postgres -c "DROP FUNCTION IF EXISTS posts_range_default_redirect();"
docker exec -it postgres_baseline psql -U postgres -d postgres -c "DROP TABLE IF EXISTS posts_range_default;"
docker exec -it postgres_baseline psql -U postgres -d postgres -c \
  "INSERT INTO posts_range (user_id, created_at, content) VALUES (2, '2026-03-01', 'should fail');"
# Expected: ERROR
```

2) Extension: pg_partman (scheduled partition management)
- Capabilities: pre-create future partitions, retention/archival, scheduled maintenance
- Setup in this project (Debian-based postgres image):
  ```bash
  docker exec -it postgres_baseline bash -lc "apt-get update && apt-get install -y postgresql-16-partman"
  docker exec -it postgres_baseline psql -U postgres -d postgres -c "CREATE EXTENSION IF NOT EXISTS pg_partman;"
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

## Clean-up

```bash
# stop and remove containers
docker-compose down

# remove volumes to free disk (will delete data!)
docker-compose down -v

# optional: remove dangling images and build cache
docker system prune -f
```



