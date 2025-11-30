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
docker exec -it app go run ./cmd/benchmark -mode=baseline -concurrency=50 -requests=1000
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

Optionally, add indexes on partitions if desired (not strictly necessary for this workshop).

---

### 6) Benchmark range

```bash
docker exec -it app go run ./cmd/benchmark -mode=range -concurrency=50 -requests=1000
```

This uses the routing that targets `posts_range` (partitioned by month).

---

### 7) Apply hash (shard) schema on each shard

```bash
docker exec -i postgres_shard_1 psql -U postgres -d postgres < sql/hash_schema.sql
docker exec -i postgres_shard_2 psql -U postgres -d postgres < sql/hash_schema.sql
docker exec -i postgres_shard_3 psql -U postgres -d postgres < sql/hash_schema.sql
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
docker exec -it app go run ./cmd/benchmark -mode=hash -concurrency=50 -requests=1000
```

Router fans out to shards based on user IDs, merges results, and sorts globally by `created_at DESC`.

---

### 10) Compare results

Fill in after your runs:

| Method   | Avg latency | P95 | QPS |
|----------|-------------|-----|-----|
| Baseline | X           | X   | X   |
| Range    | X           | X   | X   |
| Hash     | X           | X   | X   |

You should observe improved performance from baseline -> range -> hash.


