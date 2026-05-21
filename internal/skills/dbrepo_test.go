package skills_test

import (
	"context"
	"errors"
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
	"github.com/yourorg/private-coding-agent/internal/skills"
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

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(context.Background(), testDSN)
	require.NoError(t, err)
	t.Cleanup(p.Close)
	return p
}

func seedTenant(t *testing.T, p *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := p.Exec(context.Background(),
		`INSERT INTO tenants (id, slug, name) VALUES ($1,$2,$3)`,
		id, "t-"+id.String()[:8], "T")
	require.NoError(t, err)
	return id
}

func TestDBRepo_CRUD(t *testing.T) {
	p := newPool(t)
	repo := skills.NewDBRepo(p)
	ctx := context.Background()
	tid := seedTenant(t, p)

	in := &skills.DBSkill{
		ID: uuid.New(), TenantID: tid,
		SkillKey: "alpha", Description: "test",
		Body:    "BODY-ALPHA",
		Enabled: true,
	}
	out, err := repo.Insert(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "alpha", out.SkillKey)
	require.NotEmpty(t, out.ContentHash)
	require.Equal(t, skills.HashBody("BODY-ALPHA"), out.ContentHash)

	// duplicate (tenant, skill_key) -> conflict
	_, err = repo.Insert(ctx, &skills.DBSkill{
		ID: uuid.New(), TenantID: tid, SkillKey: "alpha", Body: "x",
	})
	require.ErrorIs(t, err, skills.ErrSkillKeyConflict)

	got, err := repo.GetByKey(ctx, tid, "alpha")
	require.NoError(t, err)
	require.Equal(t, out.ID, got.ID)

	// Update body + enabled
	newBody := "UPDATED"
	disabled := false
	upd, err := repo.Update(ctx, tid, "alpha", nil, &newBody, &disabled)
	require.NoError(t, err)
	require.Equal(t, "UPDATED", upd.Body)
	require.False(t, upd.Enabled)
	require.Equal(t, skills.HashBody("UPDATED"), upd.ContentHash)
	require.True(t, upd.UpdatedAt.After(out.UpdatedAt) || upd.UpdatedAt.Equal(out.UpdatedAt))

	// Disabled rows must not show in ListEnabled
	enabled, err := repo.ListEnabled(ctx, tid)
	require.NoError(t, err)
	require.Len(t, enabled, 0)

	// Re-enable and check
	on := true
	_, err = repo.Update(ctx, tid, "alpha", nil, nil, &on)
	require.NoError(t, err)
	enabled, err = repo.ListEnabled(ctx, tid)
	require.NoError(t, err)
	require.Len(t, enabled, 1)

	// List returns all
	all, err := repo.List(ctx, tid)
	require.NoError(t, err)
	require.Len(t, all, 1)

	// Delete
	require.NoError(t, repo.Delete(ctx, tid, "alpha"))
	_, err = repo.GetByKey(ctx, tid, "alpha")
	require.ErrorIs(t, err, skills.ErrSkillNotFound)
	require.ErrorIs(t, repo.Delete(ctx, tid, "alpha"), skills.ErrSkillNotFound)
}

func TestDBRepo_TenantIsolation(t *testing.T) {
	p := newPool(t)
	repo := skills.NewDBRepo(p)
	ctx := context.Background()
	tidA := seedTenant(t, p)
	tidB := seedTenant(t, p)

	for _, tid := range []uuid.UUID{tidA, tidB} {
		_, err := repo.Insert(ctx, &skills.DBSkill{
			ID: uuid.New(), TenantID: tid, SkillKey: "same-key",
			Body: "for-" + tid.String(), Enabled: true,
		})
		require.NoError(t, err)
	}

	a, err := repo.GetByKey(ctx, tidA, "same-key")
	require.NoError(t, err)
	require.Equal(t, "for-"+tidA.String(), a.Body)

	listA, err := repo.List(ctx, tidA)
	require.NoError(t, err)
	require.Len(t, listA, 1)

	// Cross-tenant lookup must miss
	_, err = repo.GetByKey(ctx, tidA, "nonexistent")
	require.ErrorIs(t, err, skills.ErrSkillNotFound)
}

func TestDBRepo_ProfileBinding(t *testing.T) {
	p := newPool(t)
	repo := skills.NewDBRepo(p)
	ctx := context.Background()
	tid := seedTenant(t, p)

	// initial: no binding
	got, err := repo.GetForProfile(ctx, tid, "coding")
	require.NoError(t, err)
	require.Empty(t, got)

	// set ordered
	require.NoError(t, repo.SetForProfile(ctx, tid, "coding", []string{"x", "y", "z"}))
	got, err = repo.GetForProfile(ctx, tid, "coding")
	require.NoError(t, err)
	require.Equal(t, []string{"x", "y", "z"}, got)

	// replace
	require.NoError(t, repo.SetForProfile(ctx, tid, "coding", []string{"only-one"}))
	got, err = repo.GetForProfile(ctx, tid, "coding")
	require.NoError(t, err)
	require.Equal(t, []string{"only-one"}, got)

	// clear
	require.NoError(t, repo.SetForProfile(ctx, tid, "coding", nil))
	got, err = repo.GetForProfile(ctx, tid, "coding")
	require.NoError(t, err)
	require.Empty(t, got)

	// other profile untouched
	require.NoError(t, repo.SetForProfile(ctx, tid, "reviewer", []string{"r1"}))
	got, err = repo.GetForProfile(ctx, tid, "coding")
	require.NoError(t, err)
	require.Empty(t, got)
	got, err = repo.GetForProfile(ctx, tid, "reviewer")
	require.NoError(t, err)
	require.Equal(t, []string{"r1"}, got)
}

func TestDBRepo_UpdateMissing(t *testing.T) {
	p := newPool(t)
	repo := skills.NewDBRepo(p)
	tid := seedTenant(t, p)
	_, err := repo.Update(context.Background(), tid, "ghost", nil, nil, nil)
	require.True(t, errors.Is(err, skills.ErrSkillNotFound))
}
