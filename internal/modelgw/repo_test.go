package modelgw_test

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
	"github.com/yourorg/private-coding-agent/internal/modelgw"
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

func TestProviderRepo_ListEnabled_SeedExists(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	repo := modelgw.NewProviderRepo(pg)
	list, err := repo.ListEnabled(ctx)
	require.NoError(t, err)
	var found bool
	for _, p := range list {
		if p.Name == "default-mock" {
			found = true
			require.Equal(t, "openai", p.Type)
			require.Equal(t, "http://mock-provider:8081", p.BaseURL)
			require.True(t, p.Enabled)
		}
	}
	require.True(t, found, "default-mock seed must exist")
}

func TestProviderRepo_GetByName_NotFound(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	repo := modelgw.NewProviderRepo(pg)
	_, err = repo.GetByName(ctx, "nope-"+fmt.Sprint(time.Now().UnixNano()))
	require.ErrorIs(t, err, modelgw.ErrProviderNotFound)
}

func TestUsageRepo_InsertAndCount(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	provRepo := modelgw.NewProviderRepo(pg)
	prov, err := provRepo.GetByName(ctx, "default-mock")
	require.NoError(t, err)

	tid := uuid.New()
	uid := uuid.New()
	repo := modelgw.NewUsageRepo(pg)
	require.NoError(t, repo.Insert(ctx, modelgw.CallEvent{
		TenantID: tid, UserID: uid,
		ProviderID: prov.ID, ProviderType: "openai", Model: "x",
		Action: "chat", Stream: false, Status: "ok",
		InputTokens: 10, OutputTokens: 20, DurationMS: 100,
		OccurredAt: time.Now(),
	}))

	n, err := repo.CountByTenant(ctx, tid)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
