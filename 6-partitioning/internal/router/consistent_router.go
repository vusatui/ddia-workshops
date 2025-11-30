package router

import (
	"context"
	"fmt"
	"sort"
	"time"

	"partitioning/ready/internal/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ConsistentHashRouter routes by consistent hashing ring.
// It minimizes key movement when shard membership changes.
type ConsistentHashRouter struct {
	Shards []*pgxpool.Pool
	Ring   *Ring
	// Table allows overriding the table name (default: posts_hash).
	Table string
}

func (r *ConsistentHashRouter) GetFeed(ctx context.Context, userIDs []int64, limit int) ([]model.Post, error) {
	if r.Ring == nil || len(r.Shards) == 0 {
		return nil, fmt.Errorf("router not initialized")
	}
	type shardResult struct {
		posts []model.Post
		err   error
	}
	perShard := make(map[int][]int64, len(r.Shards))
	for _, id := range userIDs {
		owner := r.Ring.Owner(HashUser(id))
		perShard[owner] = append(perShard[owner], id)
	}
	table := r.Table
	if table == "" {
		table = "posts_hash"
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	q := fmt.Sprintf(`
	SELECT id, user_id, created_at, content
	FROM %s
WHERE user_id = ANY($1) AND created_at >= $2
	ORDER BY created_at DESC
LIMIT $3;`, table)
	results := make(chan shardResult, len(r.Shards))

	// Distribute global LIMIT across active shards.
	active := 0
	for _, ids := range perShard {
		if len(ids) > 0 {
			active++
		}
	}
	perLimit := limit
	if active > 1 {
		perLimit = (limit + active - 1) / active
	}

	for idx, ids := range perShard {
		if len(ids) == 0 {
			results <- shardResult{}
			continue
		}
		pool := r.Shards[idx]
		go func(ids []int64, pool *pgxpool.Pool) {
			rows, err := pool.Query(ctx, q, ids, cutoff, perLimit)
			if err != nil {
				results <- shardResult{err: err}
				return
			}
			defer rows.Close()
			var ps []model.Post
			for rows.Next() {
				var p model.Post
				if err := rows.Scan(&p.ID, &p.UserID, &p.CreatedAt, &p.Content); err != nil {
					results <- shardResult{err: err}
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
	var merged []model.Post
	for i := 0; i < len(perShard); i++ {
		r := <-results
		if r.err != nil {
			return nil, r.err
		}
		merged = append(merged, r.posts...)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].CreatedAt.After(merged[j].CreatedAt) })
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}
