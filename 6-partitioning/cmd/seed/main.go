// Seed tool: populates databases for the workshop.
// - mode=baseline inserts into a single posts table (no partitioning)
// - mode=hash inserts into three shard databases based on user_id % 3
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"partitioning/ready/internal/db"

	"github.com/jackc/pgx/v5"
)

func main() {
	// CLI flags define workload characteristics.
	var mode string
	var numUsers int
	var numPosts int
	var batchSize int
	var contentSize int
	flag.StringVar(&mode, "mode", "baseline", "seed mode: baseline | hash")
	flag.IntVar(&numUsers, "users", 10000, "number of users")
	flag.IntVar(&numPosts, "posts", 1000000, "number of posts to insert")
	flag.IntVar(&batchSize, "batch", 1000, "insert batch size")
	flag.IntVar(&contentSize, "content-size", 80, "post content size (bytes/characters)")
	flag.Parse()

	ctx := context.Background()
	// Local RNG instance (no global rand.Seed); keeps randomness explicit and testable.
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	start := time.Now()
	switch mode {
	case "baseline":
		// Single-DB insert path
		if err := seedBaseline(ctx, r, numUsers, numPosts, batchSize, contentSize); err != nil {
			log.Fatalf("seed baseline failed: %v", err)
		}
	case "hash":
		// Multi-DB sharded insert path
		if err := seedHash(ctx, r, numUsers, numPosts, batchSize, contentSize); err != nil {
			log.Fatalf("seed hash failed: %v", err)
		}
	default:
		log.Fatalf("unknown mode: %s", mode)
	}
	log.Printf("done in %s", time.Since(start).Truncate(time.Millisecond))
}

// seedBaseline inserts posts into a single table using batched inserts for throughput.
func seedBaseline(ctx context.Context, r *rand.Rand, numUsers, numPosts, batchSize, contentSize int) error {
	pool, err := db.NewBaselinePool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	log.Printf("seeding baseline: users=%d posts=%d batch=%d", numUsers, numPosts, batchSize)

	// Generate timestamps uniformly across the last year.
	now := time.Now()
	yearAgo := now.Add(-365 * 24 * time.Hour)

	// Small helper to produce synthetic post content of fixed size.
	makeContent := func() string {
		var b strings.Builder
		for i := 0; i < contentSize; i++ {
			b.WriteByte(byte('a' + r.Intn(26)))
		}
		return b.String()
	}

	// Use pgx.Batch to group many INSERTs per round-trip.
	batch := &pgx.Batch{}
	pending := 0
	// flush sends the accumulated INSERTs to Postgres and resets the batch.
	flush := func() error {
		if pending == 0 {
			return nil
		}
		br := pool.SendBatch(ctx, batch)
		for i := 0; i < pending; i++ {
			if _, err := br.Exec(); err != nil {
				_ = br.Close()
				return fmt.Errorf("batch exec: %w", err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("batch close: %w", err)
		}
		batch = &pgx.Batch{}
		pending = 0
		return nil
	}

	for i := 0; i < numPosts; i++ {
		// Random user among [1..numUsers]
		userID := 1 + r.Int63n(int64(numUsers))
		// Uniform sampling over last year for created_at
		delta := r.Int63n(int64(now.Sub(yearAgo)))
		createdAt := yearAgo.Add(time.Duration(delta))
		content := makeContent()
		batch.Queue(`INSERT INTO posts (user_id, created_at, content) VALUES ($1,$2,$3)`, userID, createdAt, content)
		pending++
		if pending >= batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return nil
}

// seedHash routes each insert to one of three shards using user_id % 3.
// Each shard maintains its own pgx.Batch to minimize round-trips per shard.
func seedHash(ctx context.Context, r *rand.Rand, numUsers, numPosts, batchSize, contentSize int) error {
	pools, err := db.NewShardPools(ctx)
	if err != nil {
		return err
	}
	for _, p := range pools {
		defer p.Close()
	}

	log.Printf("seeding hash shards: users=%d posts=%d batch=%d", numUsers, numPosts, batchSize)

	// Same timestamp generation as baseline.
	now := time.Now()
	yearAgo := now.Add(-365 * 24 * time.Hour)

	makeContent := func() string {
		var b strings.Builder
		for i := 0; i < contentSize; i++ {
			b.WriteByte(byte('a' + r.Intn(26)))
		}
		return b.String()
	}

	// Per-shard batch accumulators.
	type shardBatch struct {
		batch   *pgx.Batch
		pending int
	}
	sb := []shardBatch{
		{batch: &pgx.Batch{}},
		{batch: &pgx.Batch{}},
		{batch: &pgx.Batch{}},
	}

	// flushShard sends pending INSERTs to a specific shard.
	flushShard := func(idx int) error {
		if sb[idx].pending == 0 {
			return nil
		}
		br := pools[idx].SendBatch(ctx, sb[idx].batch)
		for i := 0; i < sb[idx].pending; i++ {
			if _, err := br.Exec(); err != nil {
				_ = br.Close()
				return fmt.Errorf("shard %d exec: %w", idx, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("shard %d close: %w", idx, err)
		}
		sb[idx] = shardBatch{batch: &pgx.Batch{}, pending: 0}
		return nil
	}

	for i := 0; i < numPosts; i++ {
		// Choose shard by hashing user_id.
		userID := 1 + r.Int63n(int64(numUsers))
		delta := r.Int63n(int64(now.Sub(yearAgo)))
		createdAt := yearAgo.Add(time.Duration(delta))
		content := makeContent()

		shard := int(userID % 3)
		sb[shard].batch.Queue(`INSERT INTO posts_hash (user_id, created_at, content) VALUES ($1,$2,$3)`, userID, createdAt, content)
		sb[shard].pending++
		if sb[shard].pending >= batchSize {
			if err := flushShard(shard); err != nil {
				return err
			}
		}
	}
	for i := 0; i < 3; i++ {
		if err := flushShard(i); err != nil {
			return err
		}
	}
	return nil
}
