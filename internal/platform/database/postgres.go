package database

import (
	"context"
	"fmt"

	"github.com/bengobox/ticketing-service/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps pgxpool.Pool with additional functionality.
type Pool struct {
	*pgxpool.Pool
}

// NewPool creates a new database connection pool.
func NewPool(ctx context.Context, cfg config.PostgresConfig) (*Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("database: failed to parse config: %w", err)
	}

	poolConfig.MaxConns = int32(cfg.MaxOpenConns)
	poolConfig.MinConns = int32(cfg.MaxIdleConns)
	poolConfig.MaxConnLifetime = cfg.ConnMaxLifetime
	poolConfig.MaxConnIdleTime = cfg.ConnMaxIdleTime

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("database: failed to create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("database: failed to ping: %w", err)
	}

	return &Pool{Pool: pool}, nil
}
