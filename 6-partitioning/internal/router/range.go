package router

import (
	"context"
	"fmt"
	"time"

	"partitioning/ready/internal/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RangeRouter targets a partitioned parent table; Postgres prunes partitions
// by created_at to reduce scanned data.
type RangeRouter struct {
	DB *pgxpool.Pool
}

// GetFeed hits the partitioned table (posts_range). We align the predicate to the current month
// so the planner can prune to exactly one monthly partition.
func (r *RangeRouter) GetFeed(ctx context.Context, userIDs []int64, limit int) ([]model.Post, error) {
	if r.DB == nil {
		return nil, fmt.Errorf("db is nil")
	}
	// Use a month-aligned window for clear pruning on monthly partitions.
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	monthEnd := monthStart.AddDate(0, 1, 0)
	const q = `
	SELECT id, user_id, created_at, content
	FROM posts_range
	WHERE user_id = ANY($1) AND created_at >= $2 AND created_at < $3
	ORDER BY created_at DESC
	LIMIT $4;
	`
	rows, err := r.DB.Query(ctx, q, userIDs, monthStart, monthEnd, limit)
	if err != nil {
		return nil, fmt.Errorf("query range: %w", err)
	}
	defer rows.Close()

	var res []model.Post
	for rows.Next() {
		var p model.Post
		if err := rows.Scan(&p.ID, &p.UserID, &p.CreatedAt, &p.Content); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		res = append(res, p)
	}
	return res, rows.Err()
}
