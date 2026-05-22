package workflow_test

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
	"github.com/yourorg/private-coding-agent/internal/workflow"
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
	// Each test runs against a clean workflows + workflow_runs slate so the
	// global-namespace ListPublished doesn't surface rows from a prior test.
	// Tenants/users stay so cross-test FK targets keep working.
	_, err = p.Exec(context.Background(), `TRUNCATE TABLE workflow_runs, workflows CASCADE`)
	require.NoError(t, err)
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

const helloDSL = `
id: hello
name: Hello
steps:
  - id: greet
    assign:
      who: ${inputs.name}
outputs:
  msg: hello ${vars.who}
`

func TestRepo_CRUD_RoundTrip(t *testing.T) {
	p := newPool(t)
	repo := workflow.NewRepo(p)
	ctx := context.Background()
	tid := seedTenant(t, p)

	w, err := repo.Create(ctx, tid, "hello", "Hello", "greet user", helloDSL)
	require.NoError(t, err)
	require.Equal(t, "hello", w.Slug)
	require.Equal(t, 1, w.Version)
	require.False(t, w.Published)

	got, err := repo.Get(ctx, tid, "hello")
	require.NoError(t, err)
	require.Equal(t, w.ID, got.ID)
	require.Contains(t, got.DSLYAML, "id: hello")

	// List omits dsl_yaml
	list, err := repo.List(ctx, tid)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "", list[0].DSLYAML)

	// Update bumps version, unpublishes if published
	require.NoError(t, repo.SetPublished(ctx, tid, "hello", true))
	pub, err := repo.Get(ctx, tid, "hello")
	require.NoError(t, err)
	require.True(t, pub.Published)
	require.NotNil(t, pub.PublishedAt)

	updated, err := repo.Update(ctx, tid, "hello", "Hello v2", "edited", helloDSL+"\n# bump")
	require.NoError(t, err)
	require.Equal(t, 2, updated.Version)
	require.False(t, updated.Published, "update must force published=false")

	// Delete returns ErrNotFound on second pass
	require.NoError(t, repo.Delete(ctx, tid, "hello"))
	require.ErrorIs(t, repo.Delete(ctx, tid, "hello"), workflow.ErrNotFound)
}

func TestRepo_CrossTenantIsolation(t *testing.T) {
	p := newPool(t)
	repo := workflow.NewRepo(p)
	ctx := context.Background()
	a := seedTenant(t, p)
	b := seedTenant(t, p)

	_, err := repo.Create(ctx, a, "shared", "A", "", helloDSL)
	require.NoError(t, err)
	_, err = repo.Create(ctx, b, "shared", "B", "", helloDSL) // same slug, different tenant
	require.NoError(t, err)

	got, err := repo.Get(ctx, a, "shared")
	require.NoError(t, err)
	require.Equal(t, "A", got.Name)
	_, err = repo.Get(ctx, b, "shared")
	require.NoError(t, err)

	// Tenant a sees only its row
	listA, err := repo.List(ctx, a)
	require.NoError(t, err)
	require.Len(t, listA, 1)
	require.Equal(t, "A", listA[0].Name)
}

func TestRepo_SlugConflict(t *testing.T) {
	p := newPool(t)
	repo := workflow.NewRepo(p)
	ctx := context.Background()
	tid := seedTenant(t, p)

	_, err := repo.Create(ctx, tid, "twice", "T1", "", helloDSL)
	require.NoError(t, err)
	_, err = repo.Create(ctx, tid, "twice", "T2", "", helloDSL)
	require.True(t, errors.Is(err, workflow.ErrSlugTaken), "expected ErrSlugTaken, got %v", err)
}

func TestRepo_ListPublished(t *testing.T) {
	p := newPool(t)
	repo := workflow.NewRepo(p)
	ctx := context.Background()
	tid := seedTenant(t, p)

	_, err := repo.Create(ctx, tid, "pub", "P", "", helloDSL)
	require.NoError(t, err)
	_, err = repo.Create(ctx, tid, "unpub", "U", "", helloDSL)
	require.NoError(t, err)
	require.NoError(t, repo.SetPublished(ctx, tid, "pub", true))

	all, err := repo.ListPublished(ctx)
	require.NoError(t, err)
	found := false
	for _, w := range all {
		if w.Slug == "pub" && w.TenantID == tid {
			found = true
			require.True(t, w.Published)
			require.NotEmpty(t, w.DSLYAML, "republish needs DSL body")
		}
		require.NotEqual(t, "unpub", w.Slug, "unpublished must be excluded")
	}
	require.True(t, found, "did not find published row")
}

func TestRepo_RunLifecycle(t *testing.T) {
	p := newPool(t)
	repo := workflow.NewRepo(p)
	ctx := context.Background()
	tid := seedTenant(t, p)
	uid := uuid.New()
	w, err := repo.Create(ctx, tid, "runner", "R", "", helloDSL)
	require.NoError(t, err)

	id, err := repo.CreateRun(ctx, workflow.Run{
		TenantID: tid, UserID: uid, WorkflowID: w.ID,
		VersionAtRun: w.Version, DryRun: true, Status: "running",
		Inputs: []byte(`{"name":"alice"}`),
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, id)

	require.NoError(t, repo.FinishRun(ctx, id, workflow.StatusOK,
		[]byte(`{"msg":"hello alice"}`), "", 42))

	runs, err := repo.ListRuns(ctx, tid, w.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, "ok", runs[0].Status)
	require.True(t, runs[0].DryRun)
	require.Equal(t, 42, runs[0].DurationMS)
}
