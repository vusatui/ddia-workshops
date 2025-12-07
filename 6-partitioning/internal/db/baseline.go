// Package db provides connection helpers for baseline, range, and shard databases.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewBaselinePool creates a connection pool to the baseline Postgres instance.
// This instance hosts the non-partitioned posts table used for baseline tests.
func NewBaselinePool(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := "postgres://postgres:postgres@postgres_baseline:5432/postgres?sslmode=disable"
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse baseline dsn: %w", err)
	}
	cfg.MaxConns = 50
	// Reduce planning overhead by caching prepared statements per connection.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheStatement
	cfg.ConnConfig.StatementCacheCapacity = 256
	// Enable partition-wise optimizations (harmless for baseline, useful globally).
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
		return nil, fmt.Errorf("connect baseline: %w", err)
	}
	return pool, nil
}
