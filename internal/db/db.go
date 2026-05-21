// Package db wraps pgx pool and migration runner.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

type Pool = pgxpool.Pool

// Connect builds a pgx connection pool from the given DSN, verifies it with a
// 5s ping, and returns a ready-to-use pool. The pool is closed automatically
// if the ping fails. Caller owns the pool and must Close it on shutdown.
//
// On every new connection, the pgvector pgx codecs are registered if the
// `vector` extension is installed; absence is non-fatal so pre-migration
// callers (e.g. the migration runner itself) still work.
func Connect(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MaxConnLifetime = time.Hour
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if err := pgxvec.RegisterTypes(ctx, conn); err != nil {
			// vector extension may not be installed yet (first boot before
			// migrations run). Don't block pool creation; vector-dependent
			// queries will surface the error themselves.
			return nil
		}
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new pool: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}
