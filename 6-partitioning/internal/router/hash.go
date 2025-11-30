package router

import (
	"context"
	"fmt"
	"sort"

	"partitioning/ready/internal/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HashRouter dispatches queries to multiple shards and merges results.
// Shard selection rule: shard = user_id % 3
//
// Limitations of modulo-based sharding:
// - Rebalancing: when shard count changes, N changes => most keys remap; requires large data moves.
// - No indirection: routing is tightly coupled to N; no shard-map to remap subsets.
// - Hotspots: no virtual buckets to smooth key skew; hot users can overload a shard.
// - Migrations: no double-read/write path; rolling migrations are hard without downtime.
// - Resilience: no notion of replicas/failover in routing.
type HashRouter struct {
	Shards []*pgxpool.Pool
}

// HashUserID is a tiny helper exposing the shard mapping.
func HashUserID(id int64) int {
	return int(id % 3)
}

// GetFeed groups userIDs by shard, runs queries in parallel, merges rows,
// and returns the top-N by created_at DESC across all shards (global sort).
func (r *HashRouter) GetFeed(ctx context.Context, userIDs []int64, limit int) ([]model.Post, error) {
	if len(r.Shards) != 3 {
		return nil, fmt.Errorf("expected 3 shards, got %d", len(r.Shards))
	}
	// Group userIDs per shard
	perShard := map[int][]int64{
		0: {},
		1: {},
		2: {},
	}
	for _, id := range userIDs {
		perShard[HashUserID(id)] = append(perShard[HashUserID(id)], id)
	}

	type shardResult struct {
		posts []model.Post
		err   error
	}
	results := make(chan shardResult, 3)

	const q = `
	SELECT id, user_id, created_at, content
	FROM posts_hash
	WHERE user_id = ANY($1)
	ORDER BY created_at DESC
	LIMIT $2;
	`

	// Fan out to shards concurrently
	for shardIdx, ids := range perShard {
		if len(ids) == 0 {
			// No users on this shard; push empty result
			results <- shardResult{posts: nil, err: nil}
			continue
		}
		pool := r.Shards[shardIdx]
		go func(ids []int64, pool *pgxpool.Pool) {
			rows, err := pool.Query(ctx, q, ids, limit)
			if err != nil {
				results <- shardResult{err: fmt.Errorf("shard query: %w", err)}
				return
			}
			defer rows.Close()
			var ps []model.Post
			for rows.Next() {
				var p model.Post
				if err := rows.Scan(&p.ID, &p.UserID, &p.CreatedAt, &p.Content); err != nil {
					results <- shardResult{err: fmt.Errorf("scan: %w", err)}
					return
				}
				ps = append(ps, p)
			}
			if err := rows.Err(); err != nil {
				results <- shardResult{err: err}
				return
			}
			results <- shardResult{posts: ps}
		}(ids, pool)
	}

	// Collect and merge results from all shards
	var merged []model.Post
	for i := 0; i < 3; i++ {
		r := <-results
		if r.err != nil {
			return nil, r.err
		}
		if len(r.posts) > 0 {
			merged = append(merged, r.posts...)
		}
	}

	// Global sort and top-N cut to respect the limit across shards
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].CreatedAt.After(merged[j].CreatedAt)
	})
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}
