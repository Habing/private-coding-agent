package user_test

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
	"github.com/yourorg/private-coding-agent/internal/user"
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
	if err := pool.Retry(func() error { return db.Migrate(testDSN) }); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	os.Exit(func() int {
		defer func() { _ = pool.Purge(res) }()
		return m.Run()
	}())
}

func TestRegisterAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	defer pg.Close()

	tRepo := tenant.NewRepo(pg)
	def, err := tRepo.GetBySlug(ctx, "default")
	require.NoError(t, err)

	svc := user.NewService(user.NewRepo(pg))
	u, err := svc.Register(ctx, def.ID, "alice@example.com", "s3cret!", "Alice")
	require.NoError(t, err)
	require.NotEqual(t, "s3cret!", u.PasswordHash)

	authed, err := svc.Authenticate(ctx, def.ID, "alice@example.com", "s3cret!")
	require.NoError(t, err)
	require.Equal(t, u.ID, authed.ID)
}

func TestAuthenticate_BadPassword(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	defer pg.Close()

	tRepo := tenant.NewRepo(pg)
	def, err := tRepo.GetBySlug(ctx, "default")
	require.NoError(t, err)

	svc := user.NewService(user.NewRepo(pg))
	_, err = svc.Register(ctx, def.ID, "bob@example.com", "right-pass", "Bob")
	require.NoError(t, err)

	_, err = svc.Authenticate(ctx, def.ID, "bob@example.com", "wrong-pass")
	require.ErrorIs(t, err, user.ErrBadCredentials)
}

func TestAuthenticate_UnknownUser(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	defer pg.Close()

	tRepo := tenant.NewRepo(pg)
	def, err := tRepo.GetBySlug(ctx, "default")
	require.NoError(t, err)

	svc := user.NewService(user.NewRepo(pg))
	_, err = svc.Authenticate(ctx, def.ID, "nobody@example.com", "x")
	require.ErrorIs(t, err, user.ErrBadCredentials)
}
