package tenant_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/db"
	"github.com/yourorg/private-coding-agent/internal/tenant"
)

var testDSN string

func TestMain(m *testing.M) {
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("dockertest: %v", err)
	}
	res, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "16",
		Env: []string{
			"POSTGRES_USER=app",
			"POSTGRES_PASSWORD=app",
			"POSTGRES_DB=app",
		},
	}, func(c *docker.HostConfig) {
		c.AutoRemove = true
		c.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		log.Fatalf("run pg: %v", err)
	}
	defer func() { _ = pool.Purge(res) }()

	testDSN = fmt.Sprintf("postgres://app:app@localhost:%s/app?sslmode=disable",
		res.GetPort("5432/tcp"))

	pool.MaxWait = 60 * time.Second
	if err := pool.Retry(func() error {
		return db.Migrate(testDSN)
	}); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	code := m.Run()
	os.Exit(code)
}

func TestGetBySlug_DefaultExists(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	defer pool.Close()

	repo := tenant.NewRepo(pool)
	got, err := repo.GetBySlug(ctx, "default")
	require.NoError(t, err)
	require.Equal(t, "Default Tenant", got.Name)
}

func TestGetBySlug_NotFound(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	defer pool.Close()

	repo := tenant.NewRepo(pool)
	_, err = repo.GetBySlug(ctx, "nope")
	require.ErrorIs(t, err, tenant.ErrNotFound)
}
