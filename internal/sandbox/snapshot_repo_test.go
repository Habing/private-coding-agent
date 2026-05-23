package sandbox_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/user"
)

// setupSnapshotRepo returns a SnapshotRepo + the (tenant, user) pair to own
// any rows it writes. Reuses the dockertest container started by TestMain in
// sessionrepo_test.go.
func setupSnapshotRepo(t *testing.T) (*sandbox.SnapshotRepo, *sandbox.SessionRepo, uuid.UUID, uuid.UUID, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	tn, err := tenant.NewRepo(pg).GetBySlug(ctx, "default")
	require.NoError(t, err)
	usvc := user.NewService(user.NewRepo(pg))
	email := fmt.Sprintf("snap-%d@example.com", time.Now().UnixNano())
	u, err := usvc.Register(ctx, tn.ID, email, "irrelevant-password-XX", "SnapTester")
	require.NoError(t, err)
	return sandbox.NewSnapshotRepo(pg), sandbox.NewSessionRepo(pg), tn.ID, u.ID, pg
}

// insertSession is a small helper that creates a sandbox_sessions row directly
// so snapshot FK references resolve. Returns the new session id.
func insertSession(t *testing.T, repo *sandbox.SessionRepo, tid, uid uuid.UUID) uuid.UUID {
	t.Helper()
	sb := &sandbox.Sandbox{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Image: "pca/sandbox:base", Status: sandbox.StatusPending,
		Network:   sandbox.NetworkInternal,
		Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
	}
	require.NoError(t, repo.Insert(context.Background(), sb))
	return sb.ID
}

func TestSnapshotRepo_InsertGet(t *testing.T) {
	srepo, sessRepo, tid, uid, _ := setupSnapshotRepo(t)
	sid := insertSession(t, sessRepo, tid, uid)
	ctx := context.Background()

	snap := &sandbox.Snapshot{
		TenantID:  tid,
		UserID:    uid,
		SessionID: &sid,
		ObjectKey: tid.String() + "/" + sid.String() + "/2026-05-23T10-00-00Z.tar",
		SizeBytes: 12345,
		ImageRef:  "pca-snapshot-" + sid.String() + ":1716461400",
		Metadata:  map[string]any{"e2e": "v1"},
	}
	require.NoError(t, srepo.Insert(ctx, snap))
	require.NotEqual(t, uuid.Nil, snap.ID)
	require.False(t, snap.CreatedAt.IsZero())

	got, err := srepo.Get(ctx, tid, snap.ID)
	require.NoError(t, err)
	require.Equal(t, snap.ID, got.ID)
	require.Equal(t, tid, got.TenantID)
	require.Equal(t, uid, got.UserID)
	require.NotNil(t, got.SessionID)
	require.Equal(t, sid, *got.SessionID)
	require.Equal(t, snap.ObjectKey, got.ObjectKey)
	require.Equal(t, int64(12345), got.SizeBytes)
	require.Equal(t, snap.ImageRef, got.ImageRef)
	require.Equal(t, "v1", got.Metadata["e2e"])
}

func TestSnapshotRepo_TenantIsolation(t *testing.T) {
	srepo, sessRepo, tid, uid, _ := setupSnapshotRepo(t)
	sid := insertSession(t, sessRepo, tid, uid)
	ctx := context.Background()

	snap := &sandbox.Snapshot{
		TenantID: tid, UserID: uid, SessionID: &sid,
		ObjectKey: "k", SizeBytes: 1,
	}
	require.NoError(t, srepo.Insert(ctx, snap))

	otherTenant := uuid.New()
	_, err := srepo.Get(ctx, otherTenant, snap.ID)
	require.ErrorIs(t, err, sandbox.ErrSnapshotNotFound)
}

func TestSnapshotRepo_SessionCascadeNull(t *testing.T) {
	srepo, sessRepo, tid, uid, pg := setupSnapshotRepo(t)
	sid := insertSession(t, sessRepo, tid, uid)
	ctx := context.Background()

	snap := &sandbox.Snapshot{
		TenantID: tid, UserID: uid, SessionID: &sid,
		ObjectKey: "k", SizeBytes: 1,
	}
	require.NoError(t, srepo.Insert(ctx, snap))

	_, err := pg.Exec(ctx, `DELETE FROM sandbox_sessions WHERE id=$1`, sid)
	require.NoError(t, err)

	got, err := srepo.Get(ctx, tid, snap.ID)
	require.NoError(t, err)
	require.Nil(t, got.SessionID, "session_id should be NULL after session deletion (ON DELETE SET NULL)")
}

func TestSnapshotRepo_ListByTenant(t *testing.T) {
	srepo, sessRepo, tid, uid, _ := setupSnapshotRepo(t)
	sid := insertSession(t, sessRepo, tid, uid)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, srepo.Insert(ctx, &sandbox.Snapshot{
			TenantID: tid, UserID: uid, SessionID: &sid,
			ObjectKey: fmt.Sprintf("k%d", i),
			SizeBytes: int64(100 + i),
		}))
	}
	got, err := srepo.List(ctx, tid, nil, 50)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(got), 3)
}

func TestSnapshotRepo_ListBySession(t *testing.T) {
	srepo, sessRepo, tid, uid, _ := setupSnapshotRepo(t)
	sidA := insertSession(t, sessRepo, tid, uid)
	sidB := insertSession(t, sessRepo, tid, uid)
	ctx := context.Background()

	require.NoError(t, srepo.Insert(ctx, &sandbox.Snapshot{
		TenantID: tid, UserID: uid, SessionID: &sidA,
		ObjectKey: "kA", SizeBytes: 1,
	}))
	require.NoError(t, srepo.Insert(ctx, &sandbox.Snapshot{
		TenantID: tid, UserID: uid, SessionID: &sidB,
		ObjectKey: "kB", SizeBytes: 1,
	}))

	got, err := srepo.List(ctx, tid, &sidA, 50)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].SessionID)
	require.Equal(t, sidA, *got[0].SessionID)
}

func TestSnapshotRepo_Delete(t *testing.T) {
	srepo, sessRepo, tid, uid, _ := setupSnapshotRepo(t)
	sid := insertSession(t, sessRepo, tid, uid)
	ctx := context.Background()

	snap := &sandbox.Snapshot{
		TenantID: tid, UserID: uid, SessionID: &sid,
		ObjectKey: "k", SizeBytes: 1,
	}
	require.NoError(t, srepo.Insert(ctx, snap))

	require.NoError(t, srepo.Delete(ctx, tid, snap.ID))

	_, err := srepo.Get(ctx, tid, snap.ID)
	require.ErrorIs(t, err, sandbox.ErrSnapshotNotFound)

	require.ErrorIs(t, srepo.Delete(ctx, tid, snap.ID), sandbox.ErrSnapshotNotFound)
}
