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

// GetFeed hits the partitioned table (posts_range). The query shape matches baseline,
// but the storage engine routes the scan to relevant monthly partitions.
func (r *RangeRouter) GetFeed(ctx context.Context, userIDs []int64, limit int) ([]model.Post, error) {
	if r.DB == nil {
		return nil, fmt.Errorf("db is nil")
	}
	// Using partitioned table (in ready we name it posts_range to avoid clashing with baseline)
	// Always apply a 7-day window for clear partition pruning.
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	const q = `
	SELECT id, user_id, created_at, content
	FROM posts_range
	WHERE user_id = ANY($1) AND created_at >= $2
	ORDER BY created_at DESC
	LIMIT $3;
	`
	rows, err := r.DB.Query(ctx, q, userIDs, cutoff, limit)
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
