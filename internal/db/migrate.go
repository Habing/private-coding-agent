package db

import (
	"context"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies all pending up migrations embedded under migrations/.
// ErrNoChange is treated as success. Migrations are versioned by filename
// prefix (NNNN_*.up.sql / NNNN_*.down.sql).
//
// ctx is checked before opening the migrate connection and is honored via the
// underlying database driver for connection-level operations. Once `m.Up()`
// has started it will run to completion (golang-migrate v4 does not currently
// support cancellation mid-migration); use ctx primarily to abort before any
// work begins.
func Migrate(ctx context.Context, dsn string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("ctx: %w", err)
	}
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("iofs: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return fmt.Errorf("migrate new: %w", err)
	}
	defer m.Close()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("ctx: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
