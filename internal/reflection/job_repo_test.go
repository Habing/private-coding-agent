package reflection_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/reflection"
)

func cleanJobTables(t *testing.T, pg *pgxpool.Pool) {
	t.Helper()
	_, err := pg.Exec(context.Background(), `TRUNCATE TABLE reflection_jobs, memory_proposals CASCADE`)
	require.NoError(t, err)
}

func seedSession(t *testing.T, pg *pgxpool.Pool, tenantID, userID uuid.UUID) uuid.UUID {
	t.Helper()
	sid := uuid.New()
	_, err := pg.Exec(context.Background(),
		`INSERT INTO sessions (id, tenant_id, owner_user_id, model) VALUES ($1,$2,$3,'mock')`,
		sid, tenantID, userID)
	require.NoError(t, err)
	return sid
}

func TestJobRepo_EnqueueIdempotent(t *testing.T) {
	pg := newPool(t)
	cleanJobTables(t, pg)
	repo := reflection.NewJobRepo(pg)
	ctx := context.Background()
	tid, uid := fixtures(t, pg)
	sid := seedSession(t, pg, tid, uid)

	id1, created, err := repo.Enqueue(ctx, tid, uid, sid, 3)
	require.NoError(t, err)
	require.True(t, created)
	require.NotEqual(t, uuid.Nil, id1)

	_, created, err = repo.Enqueue(ctx, tid, uid, sid, 3)
	require.NoError(t, err)
	require.False(t, created)
}

func TestJobRepo_ListDueAndComplete(t *testing.T) {
	pg := newPool(t)
	cleanJobTables(t, pg)
	repo := reflection.NewJobRepo(pg)
	ctx := context.Background()
	tid, uid := fixtures(t, pg)
	sid := seedSession(t, pg, tid, uid)

	id, _, err := repo.Enqueue(ctx, tid, uid, sid, 3)
	require.NoError(t, err)

	due, err := repo.ListDue(ctx, 10)
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Equal(t, id, due[0].ID)

	require.NoError(t, repo.MarkProcessing(ctx, id))
	require.NoError(t, repo.MarkCompleted(ctx, id))

	due, err = repo.ListDue(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, due)
}

func TestJobRepo_MarkFailedRetriesThenTerminal(t *testing.T) {
	pg := newPool(t)
	cleanJobTables(t, pg)
	repo := reflection.NewJobRepo(pg)
	ctx := context.Background()
	tid, uid := fixtures(t, pg)
	sid := seedSession(t, pg, tid, uid)

	id, _, err := repo.Enqueue(ctx, tid, uid, sid, 2)
	require.NoError(t, err)
	require.NoError(t, repo.MarkProcessing(ctx, id))

	require.NoError(t, repo.MarkFailed(ctx, id, errors.New("boom"), time.Minute))

	var status string
	var attempts int
	var nextRun time.Time
	err = pg.QueryRow(ctx, `SELECT status, attempts, next_run_at FROM reflection_jobs WHERE id=$1`, id).
		Scan(&status, &attempts, &nextRun)
	require.NoError(t, err)
	require.Equal(t, reflection.JobStatusPending, status)
	require.Equal(t, 1, attempts)
	require.True(t, nextRun.After(time.Now()))

	require.NoError(t, repo.MarkProcessing(ctx, id))
	require.NoError(t, repo.MarkFailed(ctx, id, errors.New("again"), time.Minute))

	err = pg.QueryRow(ctx, `SELECT status, attempts FROM reflection_jobs WHERE id=$1`, id).
		Scan(&status, &attempts)
	require.NoError(t, err)
	require.Equal(t, reflection.JobStatusFailed, status)
	require.Equal(t, 2, attempts)
}

func TestJobRepo_ExpireStalePendingProposals(t *testing.T) {
	pg := newPool(t)
	cleanJobTables(t, pg)
	repo := reflection.NewJobRepo(pg)
	ctx := context.Background()
	tid, uid := fixtures(t, pg)

	_, err := pg.Exec(ctx, `
INSERT INTO memory_proposals (tenant_id, owner_user_id, type, content, confidence, status, created_at)
VALUES ($1,$2,'preference','old',0.9,'pending', now() - interval '40 days')`, tid, uid)
	require.NoError(t, err)

	n, err := repo.ExpireStalePendingProposals(ctx, 30)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	var status string
	err = pg.QueryRow(ctx, `SELECT status FROM memory_proposals WHERE tenant_id=$1`, tid).Scan(&status)
	require.NoError(t, err)
	require.Equal(t, reflection.StatusRejected, status)
}
