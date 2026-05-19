//go:build docker_integration

package sandbox_test

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/user"
)

// 注意:这个集成测试假设
// (1) Docker daemon 可达
// (2) pca/sandbox:base 镜像已 build
// (3) testDSN 已由 TestMain (sessionrepo_test.go) 准备好
// 通过 build tag `docker_integration` 隔离

func newDockerDriverForTest(t *testing.T) (*sandbox.DockerDriver, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })

	// 测试用 Redis client (Slice 2 compose 添加 redis, dockertest 也可起 redis容器)
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = rdb.Close() })

	repo := sandbox.NewSessionRepo(pg)
	d, err := sandbox.NewDockerDriver(ctx, cli, repo, rdb, sandbox.DockerDriverConfig{
		InternalNetworkName: "pca-sandbox-test-internal",
	})
	require.NoError(t, err)

	tn, err := tenant.NewRepo(pg).GetBySlug(ctx, "default")
	require.NoError(t, err)
	usvc := user.NewService(user.NewRepo(pg))
	u, err := usvc.Register(ctx, tn.ID, "drv-test@example.com"+uuid.NewString(), "irrelevant-password-XX", "Drv")
	require.NoError(t, err)

	return d, tn.ID, u.ID
}

func TestDockerDriver_Create_Success(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	sb, err := d.Create(ctx, sandbox.CreateOpts{
		TenantID:    tid,
		OwnerUserID: uid,
	})
	require.NoError(t, err)
	require.Equal(t, sandbox.StatusRunning, sb.Status)
	require.Equal(t, sandbox.DefaultImage, sb.Image)

	// cleanup container
	cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	defer cli.Close()
	cid, err := d.GetContainerIDForTest(ctx, tid, sb.ID)
	if err == nil && cid != "" {
		_ = cli.ContainerRemove(ctx, cid, container.RemoveOptions{Force: true, RemoveVolumes: true})
	}
}

func TestDockerDriver_Create_PullFailure(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	_, err := d.Create(ctx, sandbox.CreateOpts{
		TenantID:    tid,
		OwnerUserID: uid,
		Image:       "definitely-not-a-real-image:nope",
	})
	require.Error(t, err)
	// 接受 "create container:" 包装的任何下游错误
	require.Contains(t, err.Error(), "create container:")

	// 给一点时间让 Docker 异步快速失败
	time.Sleep(200 * time.Millisecond)
}

func TestDockerDriver_Get_RespectsTenant(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	got, err := d.Get(ctx, tid, sb.ID)
	require.NoError(t, err)
	require.Equal(t, sb.ID, got.ID)

	// 不同租户查不到
	_, err = d.Get(ctx, uuid.New(), sb.ID)
	require.ErrorIs(t, err, sandbox.ErrSandboxNotFound)
}

func TestDockerDriver_Destroy_Idempotent(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)

	require.NoError(t, d.Destroy(ctx, tid, sb.ID))
	// 第二次依然 nil
	require.NoError(t, d.Destroy(ctx, tid, sb.ID))

	got, err := d.Get(ctx, tid, sb.ID)
	require.NoError(t, err)
	require.Equal(t, sandbox.StatusDestroyed, got.Status)
}

func TestDockerDriver_Destroy_NotFound(t *testing.T) {
	ctx := context.Background()
	d, _, _ := newDockerDriverForTest(t)
	err := d.Destroy(ctx, uuid.New(), uuid.New())
	require.ErrorIs(t, err, sandbox.ErrSandboxNotFound)
}

func TestDockerDriver_Exec_Hello(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	res, err := d.Exec(ctx, tid, sb.ID, sandbox.ExecOpts{
		Cmd:        []string{"echo", "hello"},
		TimeoutSec: 5,
	})
	require.NoError(t, err)
	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, "hello\n", string(res.Stdout))
	require.Empty(t, res.Stderr)
	require.False(t, res.TimedOut)
	require.False(t, res.Truncated)
}

func TestDockerDriver_Exec_NonZeroExit(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	res, err := d.Exec(ctx, tid, sb.ID, sandbox.ExecOpts{
		Cmd:        []string{"false"},
		TimeoutSec: 5,
	})
	require.NoError(t, err)
	require.Equal(t, 1, res.ExitCode)
	require.False(t, res.TimedOut)
}

func TestDockerDriver_Exec_StderrSplit(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	res, err := d.Exec(ctx, tid, sb.ID, sandbox.ExecOpts{
		Cmd:        []string{"sh", "-c", "echo out; echo err >&2; exit 3"},
		TimeoutSec: 5,
	})
	require.NoError(t, err)
	require.Equal(t, 3, res.ExitCode)
	require.Equal(t, "out\n", string(res.Stdout))
	require.Equal(t, "err\n", string(res.Stderr))
}

func TestDockerDriver_Exec_Timeout(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	res, err := d.Exec(ctx, tid, sb.ID, sandbox.ExecOpts{
		Cmd:        []string{"sleep", "10"},
		TimeoutSec: 1,
	})
	require.NoError(t, err)
	require.True(t, res.TimedOut)
}

func TestDockerDriver_Exec_Truncated(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	// 200 KB of zeros from /dev/zero, well over the 128 KB MaxStreamBytes cap
	res, err := d.Exec(ctx, tid, sb.ID, sandbox.ExecOpts{
		Cmd:        []string{"sh", "-c", "head -c 200000 /dev/zero"},
		TimeoutSec: 10,
	})
	require.NoError(t, err)
	require.True(t, res.Truncated)
	require.LessOrEqual(t, len(res.Stdout), sandbox.MaxStreamBytes)
}

func TestDockerDriver_Exec_NotReady_Destroyed(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	require.NoError(t, d.Destroy(ctx, tid, sb.ID))

	_, err = d.Exec(ctx, tid, sb.ID, sandbox.ExecOpts{
		Cmd:        []string{"echo", "hi"},
		TimeoutSec: 5,
	})
	require.ErrorIs(t, err, sandbox.ErrSandboxNotReady)
}
