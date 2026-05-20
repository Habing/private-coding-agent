package toolbus_test

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
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

var testDSN string

func TestMain(m *testing.M) {
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("dockertest: %v", err)
	}
	res, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres", Tag: "16",
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

func TestInvocationRepo_InsertAndCount(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	tid := uuid.New()
	uid := uuid.New()
	repo := toolbus.NewInvocationRepo(pg)

	require.NoError(t, repo.Insert(ctx, toolbus.InvocationEvent{
		OccurredAt:   time.Now(),
		TenantID:     tid,
		UserID:       uid,
		ToolName:     "fs.read",
		Status:       "ok",
		DurationMS:   5,
		InputSHA256:  "aaaa",
		OutputSHA256: "bbbb",
	}))
	require.NoError(t, repo.Insert(ctx, toolbus.InvocationEvent{
		OccurredAt: time.Now(),
		TenantID:   tid,
		UserID:     uid,
		ToolName:   "fs.write",
		Status:     "error",
		ErrorClass: "ErrInvalidArguments",
		DurationMS: 1,
	}))

	n, err := repo.CountByTenant(ctx, tid)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	// 不同 tenant 的计数互相隔离。
	other, err := repo.CountByTenant(ctx, uuid.New())
	require.NoError(t, err)
	require.Equal(t, 0, other)
}
