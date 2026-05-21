package sandbox_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/db"
	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/user"
)

var testDSN string

func TestMain(m *testing.M) {
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("dockertest: %v", err)
	}
	res, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "pgvector/pgvector", Tag: "pg16",
		Env: []string{"POSTGRES_USER=app", "POSTGRES_PASSWORD=app", "POSTGRES_DB=app"},
	}, func(c *docker.HostConfig) {
		c.AutoRemove = true
		c.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		log.Fatalf("run pg: %v", err)
	}
	testDSN = fmt.Sprintf("postgres://app:app@localhost:%s/app?sslmode=disable",
		res.GetPort("5432/tcp"))
	pool.MaxWait = 60 * time.Second
	if err := pool.Retry(func() error { return db.Migrate(context.Background(), testDSN) }); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	os.Exit(func() int {
		defer func() { _ = pool.Purge(res) }()
		return m.Run()
	}())
}

// helper: 在 default tenant + 新 user 下创建一个 SessionRepo + 必需的 IDs
func setupRepoWithUser(t *testing.T) (*sandbox.SessionRepo, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	tn, err := tenant.NewRepo(pg).GetBySlug(ctx, "default")
	require.NoError(t, err)
	usvc := user.NewService(user.NewRepo(pg))
	email := fmt.Sprintf("sb-%d@example.com", time.Now().UnixNano())
	u, err := usvc.Register(ctx, tn.ID, email, "irrelevant-password-XX", "SbTester")
	require.NoError(t, err)
	return sandbox.NewSessionRepo(pg), tn.ID, u.ID
}

func TestSessionRepo_InsertThenGet(t *testing.T) {
	repo, tid, uid := setupRepoWithUser(t)
	ctx := context.Background()

	sb := &sandbox.Sandbox{
		ID:          uuid.New(),
		TenantID:    tid,
		OwnerUserID: uid,
		Image:       "pca/sandbox:base",
		Status:      sandbox.StatusPending,
		Network:     sandbox.NetworkInternal,
		Resources:   sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
	}
	require.NoError(t, repo.Insert(ctx, sb))

	got, err := repo.Get(ctx, tid, sb.ID)
	require.NoError(t, err)
	require.Equal(t, sb.ID, got.ID)
	require.Equal(t, sandbox.StatusPending, got.Status)
	require.Equal(t, sandbox.NetworkInternal, got.Network)
	require.Equal(t, "pca/sandbox:base", got.Image)
}

func TestSessionRepo_Get_TenantIsolation(t *testing.T) {
	repo, tid, uid := setupRepoWithUser(t)
	ctx := context.Background()

	sb := &sandbox.Sandbox{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Image: "x", Status: sandbox.StatusPending, Network: sandbox.NetworkInternal,
		Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
	}
	require.NoError(t, repo.Insert(ctx, sb))

	otherTenant := uuid.New()
	_, err := repo.Get(ctx, otherTenant, sb.ID)
	require.ErrorIs(t, err, sandbox.ErrSandboxNotFound)
}

func TestSessionRepo_SetContainerID(t *testing.T) {
	repo, tid, uid := setupRepoWithUser(t)
	ctx := context.Background()

	sb := &sandbox.Sandbox{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Image: "x", Status: sandbox.StatusPending, Network: sandbox.NetworkInternal,
		Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
	}
	require.NoError(t, repo.Insert(ctx, sb))
	require.NoError(t, repo.SetContainerID(ctx, sb.ID, "abc123"))

	got, err := repo.Get(ctx, tid, sb.ID)
	require.NoError(t, err)
	require.Equal(t, sandbox.StatusRunning, got.Status)

	cid, err := repo.GetContainerID(ctx, tid, sb.ID)
	require.NoError(t, err)
	require.Equal(t, "abc123", cid)
}

func TestSessionRepo_UpdateStatus_Terminal(t *testing.T) {
	repo, tid, uid := setupRepoWithUser(t)
	ctx := context.Background()

	sb := &sandbox.Sandbox{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Image: "x", Status: sandbox.StatusPending, Network: sandbox.NetworkInternal,
		Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
	}
	require.NoError(t, repo.Insert(ctx, sb))
	require.NoError(t, repo.UpdateStatus(ctx, sb.ID, sandbox.StatusDestroyed))

	got, err := repo.Get(ctx, tid, sb.ID)
	require.NoError(t, err)
	require.Equal(t, sandbox.StatusDestroyed, got.Status)
	require.NotNil(t, got.DestroyedAt)
}

func TestSessionRepo_ListActive(t *testing.T) {
	repo, tid, uid := setupRepoWithUser(t)
	ctx := context.Background()

	// 1 running, 1 destroyed; only running counted
	running := &sandbox.Sandbox{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Image: "x", Status: sandbox.StatusRunning, Network: sandbox.NetworkInternal,
		Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
	}
	require.NoError(t, repo.Insert(ctx, running))

	destroyed := &sandbox.Sandbox{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Image: "x", Status: sandbox.StatusPending, Network: sandbox.NetworkInternal,
		Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
	}
	require.NoError(t, repo.Insert(ctx, destroyed))
	require.NoError(t, repo.UpdateStatus(ctx, destroyed.ID, sandbox.StatusDestroyed))

	list, err := repo.ListActive(ctx)
	require.NoError(t, err)
	// at least the running one is in the result; other tests in same TestMain may add more
	var foundRunning bool
	for _, s := range list {
		if s.ID == running.ID {
			foundRunning = true
		}
		require.NotEqual(t, sandbox.StatusDestroyed, s.Status)
		require.NotEqual(t, sandbox.StatusFailed, s.Status)
	}
	require.True(t, foundRunning)
}

func TestSessionRepo_SetContainerID_RejectsNonPending(t *testing.T) {
	repo, tid, uid := setupRepoWithUser(t)
	ctx := context.Background()

	sb := &sandbox.Sandbox{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Image: "x", Status: sandbox.StatusPending, Network: sandbox.NetworkInternal,
		Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
	}
	require.NoError(t, repo.Insert(ctx, sb))
	require.NoError(t, repo.SetContainerID(ctx, sb.ID, "first-cid"))
	// 第二次应失败 (status 已经是 running)
	require.Error(t, repo.SetContainerID(ctx, sb.ID, "second-cid"))
}
