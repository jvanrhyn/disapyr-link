package db

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/001_init.sql
var migration001 string

//go:embed migrations/002_app_logs.sql
var migration002 string

// Connect creates a pgxpool connection and runs pending migrations.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return pool, nil
}

// migrate runs embedded SQL migrations in order. All statements are idempotent.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	for i, sql := range []string{migration001, migration002} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	return nil
}
