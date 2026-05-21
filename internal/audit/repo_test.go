package audit_test

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

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/db"
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
	pg, err := pgxpool.New(context.Background(), testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)
	return pg
}

func uPtr(u uuid.UUID) *uuid.UUID { return &u }
func tPtr(t time.Time) *time.Time { return &t }

func seedTenant(t *testing.T, pg *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pg.Exec(context.Background(),
		`INSERT INTO tenants (id, slug, name) VALUES ($1,$2,$3)`,
		id, "t-"+id.String()[:8], "T")
	require.NoError(t, err)
	return id
}

func TestRepo_AppendAndList_TenantScoped(t *testing.T) {
	pg := newPool(t)
	repo := audit.NewRepo(pg)
	ctx := context.Background()

	tidA := seedTenant(t, pg)
	tidB := seedTenant(t, pg)
	uidA := uuid.New()

	// 4 entries for tenant A spanning 3 actions, 1 entry for tenant B.
	mk := func(tenant uuid.UUID, action, target string, status int) audit.Entry {
		t := tenant
		return audit.Entry{
			TenantID: &t, UserID: uPtr(uidA),
			Action: action, Target: target,
			Method: "GET", Path: "/x", Status: status, DurationMS: 1,
			Metadata: map[string]any{"k": "v"},
		}
	}
	require.NoError(t, repo.Append(ctx, mk(tidA, "auth.login.success", "demo@example.com", 200)))
	require.NoError(t, repo.Append(ctx, mk(tidA, "auth.login.failure", "x@example.com", 401)))
	require.NoError(t, repo.Append(ctx, mk(tidA, "sandbox.create", "sbx-1", 201)))
	require.NoError(t, repo.Append(ctx, mk(tidA, "http_request", "", 200)))
	require.NoError(t, repo.Append(ctx, mk(tidB, "auth.login.success", "other@example.com", 200)))

	// Tenant isolation.
	got, total, err := repo.List(ctx, audit.ListFilter{TenantID: tidA})
	require.NoError(t, err)
	require.Equal(t, 4, total)
	require.Len(t, got, 4)
	for _, e := range got {
		require.NotNil(t, e.TenantID)
		require.Equal(t, tidA, *e.TenantID)
	}

	// Action prefix: "auth.login" matches both success+failure.
	got, total, err = repo.List(ctx, audit.ListFilter{TenantID: tidA, Action: "auth.login"})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, got, 2)

	// Action exact-domain: "sandbox." matches only sandbox.create.
	got, total, err = repo.List(ctx, audit.ListFilter{TenantID: tidA, Action: "sandbox."})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, "sandbox.create", got[0].Action)

	// UserID filter.
	other := uuid.New()
	got, _, err = repo.List(ctx, audit.ListFilter{TenantID: tidA, UserID: &other})
	require.NoError(t, err)
	require.Empty(t, got)
	got, _, err = repo.List(ctx, audit.ListFilter{TenantID: tidA, UserID: &uidA})
	require.NoError(t, err)
	require.Len(t, got, 4)

	// Status range: only 4xx.
	got, total, err = repo.List(ctx, audit.ListFilter{TenantID: tidA, MinStatus: 400, MaxStatus: 499})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, "auth.login.failure", got[0].Action)

	// Metadata round-trip.
	require.Equal(t, "v", got[0].Metadata["k"])
}

func TestRepo_List_TimeRange_LimitOffset(t *testing.T) {
	pg := newPool(t)
	repo := audit.NewRepo(pg)
	ctx := context.Background()

	tid := seedTenant(t, pg)
	uid := uuid.New()
	// Insert 5 rows, sleep so occurred_at default(now()) advances.
	for i := 0; i < 5; i++ {
		e := audit.Entry{
			TenantID: &tid, UserID: &uid,
			Action: fmt.Sprintf("test.event.%d", i),
			Method: "POST", Path: "/x", Status: 200, DurationMS: 1,
		}
		require.NoError(t, repo.Append(ctx, e))
		time.Sleep(5 * time.Millisecond)
	}

	// Limit + offset paging.
	page1, total, err := repo.List(ctx, audit.ListFilter{TenantID: tid, Limit: 2, Offset: 0})
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, page1, 2)
	page2, _, err := repo.List(ctx, audit.ListFilter{TenantID: tid, Limit: 2, Offset: 2})
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.NotEqual(t, page1[0].Action, page2[0].Action,
		"pages should not overlap")

	// Time range: From = "now" excludes everything.
	future := time.Now().Add(time.Hour)
	got, total, err := repo.List(ctx, audit.ListFilter{TenantID: tid, From: tPtr(future)})
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, got)

	// Time range: To = "now - 1h" excludes everything.
	past := time.Now().Add(-time.Hour)
	got, total, err = repo.List(ctx, audit.ListFilter{TenantID: tid, To: tPtr(past)})
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, got)
}

func TestRepo_List_MissingTenantErrors(t *testing.T) {
	pg := newPool(t)
	repo := audit.NewRepo(pg)
	_, _, err := repo.List(context.Background(), audit.ListFilter{})
	require.Error(t, err)
}

func TestRepo_List_LimitClampedToMax(t *testing.T) {
	pg := newPool(t)
	repo := audit.NewRepo(pg)
	tid := seedTenant(t, pg)
	_, _, err := repo.List(context.Background(), audit.ListFilter{TenantID: tid, Limit: 99999})
	require.NoError(t, err, "should accept oversized limit by clamping it")
}
