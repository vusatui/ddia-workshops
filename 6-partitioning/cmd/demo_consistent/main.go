package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"sync"
	"time"

	"partitioning/ready/internal/db"
	"partitioning/ready/internal/router"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This demo shows consistent hashing end-to-end:
// 1) Seed data across 3 shards using a consistent hashing ring (3 nodes)
// 2) Run a small benchmark (read) using the ring(3)
// 3) "Add a shard": build ring(4) where shard #3 points to postgres_baseline
// 4) Migrate moved users' rows from old shard to the new owner (insert-select, then delete)
// 5) Run the same benchmark using ring(4) and compare stats
//
// To avoid clashing with modulo-based example, we use a separate table name: posts_hash_ch
func main() {
	var users int
	var postsPerUser int
	var batch int
	var requests int
	var limit int
	var concurrency int
	flag.IntVar(&users, "users", 2000, "number of users to seed/migrate")
	flag.IntVar(&postsPerUser, "posts-per-user", 3, "posts per user (demo scale)")
	flag.IntVar(&batch, "batch", 500, "insert batch size")
	flag.IntVar(&requests, "requests", 300, "read requests per phase")
	flag.IntVar(&limit, "limit", 50, "feed limit")
	flag.IntVar(&concurrency, "concurrency", 20, "concurrent readers for benchmark")
	flag.Parse()

	ctx := context.Background()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Pools: shards 0..2 from NewShardPools + baseline as shard #3
	shardPools, err := db.NewShardPools(ctx)
	if err != nil {
		log.Fatalf("connect shards: %v", err)
	}
	basePool, err := db.NewBaselinePool(ctx)
	if err != nil {
		log.Fatalf("connect baseline: %v", err)
	}
	defer basePool.Close()
	for _, p := range shardPools {
		defer p.Close()
	}
	pools3 := shardPools
	pools4 := append(append([]*pgxpool.Pool{}, shardPools[0], shardPools[1], shardPools[2]), basePool)

	// Ensure demo tables exist on all 4 pools
	ensureDemoTables(ctx, append([]*pgxpool.Pool{}, pools4...))

	// Build ring(3) and seed demo data
	ring3 := router.NewRing(200)
	ring3.Build([]int{0, 1, 2})
	log.Printf("[phase:seed-3] users=%d postsPerUser=%d", users, postsPerUser)
	if err := seedDemo(ctx, rng, ring3, pools3, users, postsPerUser, batch); err != nil {
		log.Fatalf("seed 3 shards failed: %v", err)
	}

	// Benchmark reads on ring(3)
	rtr3 := &router.ConsistentHashRouter{Shards: pools3, Ring: ring3, Table: "posts_hash_ch"}
	log.Printf("[phase:bench-3] requests=%d concurrency=%d limit=%d", requests, concurrency, limit)
	runBench(ctx, rng, rtr3, users, requests, concurrency, limit)

	// Prepare ring(4) with baseline as shard #3
	ring4 := router.NewRing(200)
	ring4.Build([]int{0, 1, 2, 3})

	// Estimate moved keys and migrate
	moved := estimateMoved(users, ring3, ring4)
	log.Printf("[phase:migrate] estimated moved users: %.2f%% (%d/%d)", 100*float64(moved)/float64(users), moved, users)
	if err := migrateUsers(ctx, ring3, ring4, pools4, users); err != nil {
		log.Fatalf("migrate failed: %v", err)
	}

	// Benchmark reads on ring(4)
	rtr4 := &router.ConsistentHashRouter{Shards: pools4, Ring: ring4, Table: "posts_hash_ch"}
	log.Printf("[phase:bench-4] requests=%d concurrency=%d limit=%d", requests, concurrency, limit)
	runBench(ctx, rng, rtr4, users, requests, concurrency, limit)
}

func ensureDemoTables(ctx context.Context, pools []*pgxpool.Pool) {
	const schema = `
	CREATE TABLE IF NOT EXISTS posts_hash_ch (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL,
	created_at TIMESTAMP NOT NULL,
	content TEXT NOT NULL
	);`
	for i, p := range pools {
		if _, err := p.Exec(ctx, schema); err != nil {
			log.Fatalf("ensure table on shard %d: %v", i, err)
		}
	}
}

func seedDemo(ctx context.Context, rng *rand.Rand, ring *router.Ring, pools []*pgxpool.Pool, users, postsPerUser, batchSize int) error {
	now := time.Now()
	yearAgo := now.Add(-365 * 24 * time.Hour)

	type shardBatch struct {
		batch   *pgx.Batch
		pending int
	}
	sb := make([]shardBatch, len(pools))
	for i := range sb {
		sb[i] = shardBatch{batch: &pgx.Batch{}}
	}
	flushShard := func(idx int) error {
		if sb[idx].pending == 0 {
			return nil
		}
		br := pools[idx].SendBatch(ctx, sb[idx].batch)
		for i := 0; i < sb[idx].pending; i++ {
			if _, err := br.Exec(); err != nil {
				_ = br.Close()
				return fmt.Errorf("exec shard %d: %w", idx, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("close shard %d: %w", idx, err)
		}
		sb[idx] = shardBatch{batch: &pgx.Batch{}, pending: 0}
		return nil
	}

	makeContent := func() string {
		const letters = "abcdefghijklmnopqrstuvwxyz"
		b := make([]byte, 40)
		for i := range b {
			b[i] = letters[rng.Intn(len(letters))]
		}
		return string(b)
	}

	for u := 1; u <= users; u++ {
		owner := ring.Owner(router.HashUser(int64(u)))
		for k := 0; k < postsPerUser; k++ {
			delta := rng.Int63n(int64(now.Sub(yearAgo)))
			createdAt := yearAgo.Add(time.Duration(delta))
			sb[owner].batch.Queue(`INSERT INTO posts_hash_ch (user_id, created_at, content) VALUES ($1,$2,$3)`,
				int64(u), createdAt, makeContent())
			sb[owner].pending++
			if sb[owner].pending >= batchSize {
				if err := flushShard(owner); err != nil {
					return err
				}
			}
		}
	}
	for i := range sb {
		if err := flushShard(i); err != nil {
			return err
		}
	}
	return nil
}

func estimateMoved(users int, oldRing, newRing *router.Ring) int {
	var moved int
	for u := 1; u <= users; u++ {
		key := router.HashUser(int64(u))
		if oldRing.Owner(key) != newRing.Owner(key) {
			moved++
		}
	}
	return moved
}

func migrateUsers(ctx context.Context, oldRing, newRing *router.Ring, pools []*pgxpool.Pool, users int) error {
	// For each user that changes ownership, move rows from old shard to new shard.
	for u := 1; u <= users; u++ {
		key := router.HashUser(int64(u))
		old := oldRing.Owner(key)
		new := newRing.Owner(key)
		if old == new {
			continue
		}
		// Insert into new then delete from old to avoid losing data on failure.
		if _, err := pools[new].Exec(ctx,
			`INSERT INTO posts_hash_ch (user_id, created_at, content)
			 SELECT user_id, created_at, content FROM posts_hash_ch WHERE user_id = $1`, int64(u)); err != nil {
			return fmt.Errorf("insert new shard %d user %d: %w", new, u, err)
		}
		if _, err := pools[old].Exec(ctx,
			`DELETE FROM posts_hash_ch WHERE user_id = $1`, int64(u)); err != nil {
			return fmt.Errorf("delete old shard %d user %d: %w", old, u, err)
		}
	}
	return nil
}

func runBench(ctx context.Context, rng *rand.Rand, rtr *router.ConsistentHashRouter, users, requests, concurrency, limit int) {
	type result struct {
		d time.Duration
		e error
	}
	jobs := make(chan struct{}, requests)
	results := make(chan result, requests)
	for i := 0; i < requests; i++ {
		jobs <- struct{}{}
	}
	close(jobs)
	var wg sync.WaitGroup
	wg.Add(concurrency)
	startAll := time.Now()
	for w := 0; w < concurrency; w++ {
		go func() {
			defer wg.Done()
			for range jobs {
				ids := make([]int64, 0, 10)
				for len(ids) < 10 {
					ids = append(ids, 1+rng.Int63n(int64(users)))
				}
				start := time.Now()
				_, err := rtr.GetFeed(ctx, ids, limit)
				results <- result{d: time.Since(start), e: err}
			}
		}()
	}
	wg.Wait()
	close(results)
	total := time.Since(startAll)
	var lats []time.Duration
	var errs int
	for r := range results {
		if r.e != nil {
			errs++
			continue
		}
		lats = append(lats, r.d)
	}
	if len(lats) == 0 {
		log.Printf("no success; errs=%d", errs)
		return
	}
	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
	var sum time.Duration
	for _, d := range lats {
		sum += d
	}
	avg := time.Duration(int64(sum) / int64(len(lats)))
	p95 := lats[int(float64(len(lats))*0.95)-1]
	qps := float64(len(lats)) / total.Seconds()
	log.Printf("requests=%d errs=%d avg=%s p95=%s qps=%.2f", len(lats), errs, avg.Truncate(time.Microsecond), p95.Truncate(time.Microsecond), qps)
}
