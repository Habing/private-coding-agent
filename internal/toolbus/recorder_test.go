package toolbus_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

func TestInvocationRecorder_SurvivesCanceledCallerCtx(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	var errs []error
	var mu sync.Mutex
	rec := toolbus.NewInvocationRecorder(toolbus.NewInvocationRepo(pg), func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	})

	tid := uuid.New()
	rec.Record(toolbus.InvocationEvent{
		OccurredAt: time.Now(),
		TenantID:   tid,
		UserID:     uuid.New(),
		ToolName:   "fs.read",
		Status:     "ok",
		DurationMS: 1,
	})

	mu.Lock()
	require.Empty(t, errs)
	mu.Unlock()

	n, err := toolbus.NewInvocationRepo(pg).CountByTenant(ctx, tid)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
