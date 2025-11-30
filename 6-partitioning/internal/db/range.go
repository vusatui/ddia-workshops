package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewRangePool creates a connection pool to the same Postgres instance as baseline,
// but the router will target the partitioned table (posts_range) for range benchmarks.
func NewRangePool(ctx context.Context) (*pgxpool.Pool, error) {
	// Range partitions live in the baseline Postgres instance
	dsn := "postgres://postgres:postgres@postgres_baseline:5432/postgres?sslmode=disable"
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect range: %w", err)
	}
	return pool, nil
}
