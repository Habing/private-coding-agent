package audit_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/audit"
)

// writeN appends n entries so verify tests have a known chain to inspect.
func writeN(t *testing.T, repo *audit.Repo, tid uuid.UUID, uid uuid.UUID, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		e := audit.Entry{
			TenantID: &tid, UserID: &uid,
			Action: fmt.Sprintf("verify.test.%d", i),
			Method: "POST", Path: "/x", Status: 200, DurationMS: 1,
			Metadata: map[string]any{"i": i},
		}
		require.NoError(t, repo.Append(ctx, e))
	}
}

func TestRepo_Verify_CleanChain(t *testing.T) {
	truncateAudit(t)
	pg := newPool(t)
	repo := audit.NewRepo(pg)
	tid := seedTenant(t, pg)
	uid := uuid.New()
	writeN(t, repo, tid, uid, 10)

	res, err := repo.Verify(context.Background(), 0)
	require.NoError(t, err)
	require.True(t, res.OK)
	require.Equal(t, 10, res.RowsChecked)
	require.Equal(t, 0, res.PreChainRows)
	require.NotZero(t, res.ChainStartID)
	require.NotZero(t, res.ChainEndID)
	require.GreaterOrEqual(t, res.ChainEndID, res.ChainStartID)
	require.Zero(t, res.FirstBrokenID)
	require.Empty(t, res.Reason)
}

func TestRepo_Verify_TamperedMetadata(t *testing.T) {
	truncateAudit(t)
	pg := newPool(t)
	repo := audit.NewRepo(pg)
	tid := seedTenant(t, pg)
	uid := uuid.New()
	writeN(t, repo, tid, uid, 5)

	var targetID int64
	require.NoError(t, pg.QueryRow(context.Background(),
		`SELECT id FROM audit_log ORDER BY id LIMIT 1 OFFSET 2`).Scan(&targetID))

	_, err := pg.Exec(context.Background(),
		`UPDATE audit_log SET metadata=$1::jsonb WHERE id=$2`,
		`{"tampered":"yes"}`, targetID)
	require.NoError(t, err)

	res, err := repo.Verify(context.Background(), 0)
	require.NoError(t, err)
	require.False(t, res.OK)
	require.Equal(t, targetID, res.FirstBrokenID)
	require.Equal(t, "entry_hash_mismatch", res.Reason)
	require.NotEmpty(t, res.ExpectedHash)
	require.NotEmpty(t, res.ActualHash)
	require.NotEqual(t, res.ExpectedHash, res.ActualHash)
}

func TestRepo_Verify_TamperedPrevHash(t *testing.T) {
	truncateAudit(t)
	pg := newPool(t)
	repo := audit.NewRepo(pg)
	tid := seedTenant(t, pg)
	uid := uuid.New()
	writeN(t, repo, tid, uid, 5)

	var targetID int64
	require.NoError(t, pg.QueryRow(context.Background(),
		`SELECT id FROM audit_log ORDER BY id LIMIT 1 OFFSET 2`).Scan(&targetID))

	// Flip one byte of prev_hash so the chain link breaks at this row.
	_, err := pg.Exec(context.Background(), `
UPDATE audit_log
SET prev_hash = decode('ff00000000000000000000000000000000000000000000000000000000000000','hex')
WHERE id=$1`, targetID)
	require.NoError(t, err)

	res, err := repo.Verify(context.Background(), 0)
	require.NoError(t, err)
	require.False(t, res.OK)
	require.Equal(t, targetID, res.FirstBrokenID)
	require.Equal(t, "prev_hash_mismatch", res.Reason)
}

func TestRepo_Verify_FromIDSkipsBrokenRow(t *testing.T) {
	truncateAudit(t)
	pg := newPool(t)
	repo := audit.NewRepo(pg)
	tid := seedTenant(t, pg)
	uid := uuid.New()
	writeN(t, repo, tid, uid, 10)

	var brokenID, afterID int64
	require.NoError(t, pg.QueryRow(context.Background(),
		`SELECT id FROM audit_log ORDER BY id LIMIT 1 OFFSET 2`).Scan(&brokenID))
	require.NoError(t, pg.QueryRow(context.Background(),
		`SELECT id FROM audit_log ORDER BY id LIMIT 1 OFFSET 5`).Scan(&afterID))

	_, err := pg.Exec(context.Background(),
		`UPDATE audit_log SET metadata=$1::jsonb WHERE id=$2`,
		`{"tampered":"yes"}`, brokenID)
	require.NoError(t, err)

	// Full verify catches it...
	res, err := repo.Verify(context.Background(), 0)
	require.NoError(t, err)
	require.False(t, res.OK)
	require.Equal(t, brokenID, res.FirstBrokenID)

	// ...but starting from a later id walks an internally-consistent suffix.
	// The suffix has its own running prev=zero, so it verifies as a fresh chain.
	res, err = repo.Verify(context.Background(), afterID)
	require.NoError(t, err)
	require.True(t, res.OK, "from_id past the break should report OK on the suffix")
}

func TestRepo_Verify_SkipsPreChainRows(t *testing.T) {
	truncateAudit(t)
	pg := newPool(t)
	repo := audit.NewRepo(pg)
	tid := seedTenant(t, pg)
	uid := uuid.New()
	ctx := context.Background()

	// Two "pre-chain" rows: prev_hash and entry_hash both zero, written via raw
	// INSERT that bypasses Repo.Append (simulating rows that already lived in
	// audit_log before migration 0021).
	for i := 0; i < 2; i++ {
		_, err := pg.Exec(ctx, `
INSERT INTO audit_log (occurred_at, tenant_id, user_id, action, target, method, path, status, duration_ms, metadata)
VALUES (NOW(), $1, $2, $3, '', 'POST', '/x', 200, 1, '{}'::jsonb)`,
			tid, uid, fmt.Sprintf("prechain.%d", i))
		require.NoError(t, err)
	}

	// Then 4 real chain rows via Repo.Append.
	writeN(t, repo, tid, uid, 4)

	res, err := repo.Verify(ctx, 0)
	require.NoError(t, err)
	require.True(t, res.OK)
	require.Equal(t, 2, res.PreChainRows)
	require.Equal(t, 4, res.RowsChecked)
}

func TestRepo_Verify_EmptyTable(t *testing.T) {
	truncateAudit(t)
	repo := audit.NewRepo(newPool(t))

	res, err := repo.Verify(context.Background(), 0)
	require.NoError(t, err)
	require.True(t, res.OK, "empty audit_log verifies vacuously")
	require.Zero(t, res.RowsChecked)
	require.Zero(t, res.PreChainRows)
}
