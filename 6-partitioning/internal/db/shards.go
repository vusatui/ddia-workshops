package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewShardPools connects to three independent Postgres instances (shards).
// The application routes rows to shards by user_id % 3.
func NewShardPools(ctx context.Context) ([]*pgxpool.Pool, error) {
	hosts := []string{
		"postgres_shard_1",
		"postgres_shard_2",
		"postgres_shard_3",
	}
	pools := make([]*pgxpool.Pool, 0, len(hosts))
	for _, h := range hosts {
		dsn := fmt.Sprintf("postgres://postgres:postgres@%s:5432/postgres?sslmode=disable", h)
		cfg, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			return nil, fmt.Errorf("parse shard %s dsn: %w", h, err)
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
			return nil, fmt.Errorf("connect shard %s: %w", h, err)
		}
		pools = append(pools, pool)
	}
	return pools, nil
}
