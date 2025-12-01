// Package db provides connection helpers for baseline, range, and shard databases.
package db

import (
	"context"
	"fmt"

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
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect baseline: %w", err)
	}
	return pool, nil
}
