package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewRangePool creates a connection pool to the same Postgres instance as baseline,
// but the router will target the partitioned table (posts_range) for range benchmarks.
func NewRangePool(ctx context.Context) (*pgxpool.Pool, error) {
	// Range partitions live in the baseline Postgres instance
	dsn := "postgres://postgres:postgres@postgres_baseline:5432/postgres?sslmode=disable"
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse range dsn: %w", err)
	}
	cfg.MaxConns = 50
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheStatement
	cfg.ConnConfig.StatementCacheCapacity = 256
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, "SET enable_partitionwise_join = on"); err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, "SET enable_partitionwise_aggregate = on"); err != nil {
			return err
		}
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect range: %w", err)
	}
	return pool, nil
}
