package audit_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/audit"
)

// truncateAudit clears audit_log so chain tests can assert genesis-relative
// invariants without interference from prior tests in the shared DB.
func truncateAudit(t *testing.T) {
	t.Helper()
	pg := newPool(t)
	_, err := pg.Exec(context.Background(), `TRUNCATE audit_log RESTART IDENTITY`)
	require.NoError(t, err)
}

// readChain reads back audit_log in id order so chain tests can inspect
// prev_hash and entry_hash on the actual stored bytes (List does not surface
// these columns).
type chainRow struct {
	id        int64
	prevHash  []byte
	entryHash []byte
}

func readChain(t *testing.T) []chainRow {
	t.Helper()
	pg := newPool(t)
	rows, err := pg.Query(context.Background(),
		`SELECT id, prev_hash, entry_hash FROM audit_log ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	out := []chainRow{}
	for rows.Next() {
		var r chainRow
		require.NoError(t, rows.Scan(&r.id, &r.prevHash, &r.entryHash))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

func TestRepo_Append_GenesisRow(t *testing.T) {
	truncateAudit(t)
	pg := newPool(t)
	repo := audit.NewRepo(pg)
	tid := seedTenant(t, pg)
	uid := uuid.New()

	e := audit.Entry{
		TenantID: &tid, UserID: &uid,
		Action: "genesis.test", Method: "POST", Path: "/x",
		Status: 200, DurationMS: 1,
	}
	require.NoError(t, repo.Append(context.Background(), e))

	rows := readChain(t)
	require.Len(t, rows, 1)
	require.Equal(t, audit.ZeroHash(), rows[0].prevHash, "genesis prev_hash must be zero")
	require.Len(t, rows[0].entryHash, audit.HashSize)
	require.False(t, audit.IsZeroHash(rows[0].entryHash), "genesis entry_hash must be non-zero")
}

func TestRepo_Append_ChainsHashes(t *testing.T) {
	truncateAudit(t)
	pg := newPool(t)
	repo := audit.NewRepo(pg)
	tid := seedTenant(t, pg)
	uid := uuid.New()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		e := audit.Entry{
			TenantID: &tid, UserID: &uid,
			Action: fmt.Sprintf("chain.test.%d", i),
			Method: "POST", Path: "/x", Status: 200, DurationMS: 1,
			Metadata: map[string]any{"i": i},
		}
		require.NoError(t, repo.Append(ctx, e))
	}

	rows := readChain(t)
	require.Len(t, rows, 5)
	require.Equal(t, audit.ZeroHash(), rows[0].prevHash)
	for i := 1; i < len(rows); i++ {
		require.Equal(t, rows[i-1].entryHash, rows[i].prevHash,
			"row %d prev_hash must equal row %d entry_hash", i, i-1)
	}
}

func TestRepo_Append_ConcurrentChain(t *testing.T) {
	truncateAudit(t)
	pg := newPool(t)
	repo := audit.NewRepo(pg)
	tid := seedTenant(t, pg)
	uid := uuid.New()
	ctx := context.Background()

	const goroutines = 20
	const perGoroutine = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				e := audit.Entry{
					TenantID: &tid, UserID: &uid,
					Action: fmt.Sprintf("concurrent.%d.%d", g, i),
					Method: "POST", Path: "/x", Status: 200, DurationMS: 1,
				}
				require.NoError(t, repo.Append(ctx, e))
			}
		}(g)
	}
	wg.Wait()

	rows := readChain(t)
	require.Len(t, rows, goroutines*perGoroutine)
	require.Equal(t, audit.ZeroHash(), rows[0].prevHash)
	for i := 1; i < len(rows); i++ {
		require.Equal(t, rows[i-1].entryHash, rows[i].prevHash,
			"concurrent write broke chain at row %d", i)
	}
}
