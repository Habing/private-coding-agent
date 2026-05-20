package modelgw_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func TestUsageRecorder_SurvivesCanceledCallerCtx(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	provRepo := modelgw.NewProviderRepo(pg)
	prov, err := provRepo.GetByName(ctx, "default-mock")
	require.NoError(t, err)

	var errs []error
	var mu sync.Mutex
	rec := modelgw.NewUsageRecorder(modelgw.NewUsageRepo(pg), func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	})

	tid := uuid.New()
	rec.Record(modelgw.CallEvent{
		TenantID: tid, UserID: uuid.New(),
		ProviderID: prov.ID, ProviderType: "openai", Model: "x",
		Action: "chat", Status: "ok", DurationMS: 1,
		OccurredAt: time.Now(),
	})

	require.Empty(t, errs)
	n, err := modelgw.NewUsageRepo(pg).CountByTenant(ctx, tid)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
