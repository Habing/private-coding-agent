package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/db"
)

const fakeDSN = "postgres://app:app@localhost:1/app?sslmode=disable"

func TestMigrate_RespectsCanceledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := db.Migrate(ctx, fakeDSN)
	require.ErrorIs(t, err, context.Canceled)
}

func TestMigrate_RespectsDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	err := db.Migrate(ctx, fakeDSN)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
