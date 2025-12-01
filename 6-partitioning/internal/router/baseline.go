// Package router contains routing logic for different partitioning strategies.
package router

import (
	"context"
	"fmt"
	"time"

	"partitioning/ready/internal/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BaselineRouter targets the single non-partitioned posts table.
type BaselineRouter struct {
	DB *pgxpool.Pool
}

// GetFeed returns newest posts for the provided set of userIDs.
// This is a straightforward query against the monolithic table.
func (r *BaselineRouter) GetFeed(ctx context.Context, userIDs []int64, limit int) ([]model.Post, error) {
	if r.DB == nil {
		return nil, fmt.Errorf("db is nil")
	}
	// Always apply a 7-day time window to highlight partitioning impact consistently.
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	const q = `
	SELECT id, user_id, created_at, content
	FROM posts
	WHERE user_id = ANY($1) AND created_at >= $2
	ORDER BY created_at DESC
	LIMIT $3;
	`
	rows, err := r.DB.Query(ctx, q, userIDs, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("query baseline: %w", err)
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
