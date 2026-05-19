//go:build docker_integration

package sandbox_test

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
)

func TestReconciler_MarksDeadContainerDestroyed(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	cid, err := d.GetContainerIDForTest(ctx, tid, sb.ID)
	require.NoError(t, err)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()

	require.NoError(t, cli.ContainerRemove(ctx, cid, container.RemoveOptions{Force: true}))

	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	defer pg.Close()
	repo := sandbox.NewSessionRepo(pg)

	require.NoError(t, sandbox.RunReconciler(ctx, repo, cli))

	got, err := repo.Get(ctx, tid, sb.ID)
	require.NoError(t, err)
	require.Equal(t, sandbox.StatusDestroyed, got.Status)
}
