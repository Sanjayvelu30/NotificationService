package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}

	cfg.MaxConns = 10
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute
	// CRITICAL SYSTEM ARCHITECTURE DECISION:
	// We force pgx to use Simple Protocol Mode (pgx.QueryExecModeSimpleProtocol).
	// This prevents the driver from caching prepared statements.
	// When executing queries through transaction-level connection poolers (like PgBouncer
	// on Supabase Port 6543), consecutive queries are routed randomly to different
	// server connections. Utilizing Extended Protocol/Prepared Statements here would trigger
	// "prepared statement already exists" (SQLSTATE 42P05) or mismatch compilation errors.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}
