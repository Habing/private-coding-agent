//go:build docker_integration

package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/db"
	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
	"github.com/yourorg/private-coding-agent/internal/user"
)

var testDSN string

func TestMain(m *testing.M) {
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatal(err)
	}
	res, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "pgvector/pgvector", Tag: "pg16",
		Env: []string{"POSTGRES_USER=app", "POSTGRES_PASSWORD=app", "POSTGRES_DB=app"},
	}, func(c *docker.HostConfig) {
		c.AutoRemove = true
		c.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		log.Fatal(err)
	}
	testDSN = fmt.Sprintf("postgres://app:app@localhost:%s/app?sslmode=disable", res.GetPort("5432/tcp"))
	pool.MaxWait = 60 * time.Second
	if err := pool.Retry(func() error { return db.Migrate(context.Background(), testDSN) }); err != nil {
		log.Fatal(err)
	}
	os.Exit(func() int { defer func() { _ = pool.Purge(res) }(); return m.Run() }())
}

func newDriverForToolsTest(t *testing.T) (*sandbox.DockerDriver, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })

	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = rdb.Close() })

	repo := sandbox.NewSessionRepo(pg)
	d, err := sandbox.NewDockerDriver(ctx, cli, repo, rdb, sandbox.DockerDriverConfig{
		InternalNetworkName: "pca-sandbox-tools-test",
	})
	require.NoError(t, err)

	tn, err := tenant.NewRepo(pg).GetBySlug(ctx, "default")
	require.NoError(t, err)
	usvc := user.NewService(user.NewRepo(pg))
	u, err := usvc.Register(ctx, tn.ID,
		"tools-it-"+uuid.NewString()+"@example.com", "irrelevant-password", "tools")
	require.NoError(t, err)
	return d, tn.ID, u.ID
}

func TestFSRead_Integration(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDriverForToolsTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	require.NoError(t, d.WriteFile(ctx, tid, sb.ID, "hello.txt", []byte("hi from fs.read")))

	tool := tools.NewFSRead(d)
	in, _ := json.Marshal(map[string]string{"sandbox_id": sb.ID.String(), "path": "hello.txt"})
	out, err := tool.Invoke(ctx, tid, uid, in)
	require.NoError(t, err)
	require.Contains(t, string(out), "hi from fs.read")
}

func TestFSWriteThenList_Integration(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDriverForToolsTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	writeTool := tools.NewFSWrite(d)
	in, _ := json.Marshal(map[string]string{
		"sandbox_id": sb.ID.String(), "path": "src/main.go", "content": "package main",
	})
	_, err = writeTool.Invoke(ctx, tid, uid, in)
	require.NoError(t, err)

	listTool := tools.NewFSList(d)
	lin, _ := json.Marshal(map[string]string{"sandbox_id": sb.ID.String(), "path": "."})
	lout, err := listTool.Invoke(ctx, tid, uid, lin)
	require.NoError(t, err)
	require.Contains(t, string(lout), "src")
}
