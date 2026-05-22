package reflection_test

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
	"github.com/yourorg/private-coding-agent/internal/reflection"
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

func fixtures(t *testing.T, pg *pgxpool.Pool) (tenantID, userID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	tenantID = uuid.New()
	userID = uuid.New()
	_, err := pg.Exec(ctx,
		`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $3)`,
		tenantID, "t-"+tenantID.String()[:8], "T")
	require.NoError(t, err)
	_, err = pg.Exec(ctx,
		`INSERT INTO users (id, tenant_id, email, password_hash) VALUES ($1, $2, $3, '')`,
		userID, tenantID, "u-"+userID.String()[:8]+"@example.com")
	require.NoError(t, err)
	return
}

func newProp(tid, uid uuid.UUID, content string, confidence float32) *reflection.MemoryProposal {
	return &reflection.MemoryProposal{
		TenantID:    tid,
		OwnerUserID: uid,
		Type:        reflection.TypePreference,
		Content:     content,
		Tags:        []string{"e2e"},
		Confidence:  confidence,
		Status:      reflection.StatusPending,
	}
}

func TestRepo_InsertGet(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := reflection.NewRepo(pg)

	in := newProp(tid, uid, "loves generics", 0.5)
	out, err := repo.Insert(ctx, in)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, out.ID)
	require.Equal(t, reflection.StatusPending, out.Status)
	require.Equal(t, []string{"e2e"}, out.Tags)
	require.WithinDuration(t, time.Now(), out.CreatedAt, 10*time.Second)

	got, err := repo.Get(ctx, tid, out.ID)
	require.NoError(t, err)
	require.Equal(t, out.ID, got.ID)

	// Cross-tenant get returns not-found.
	_, err = repo.Get(ctx, uuid.New(), out.ID)
	require.ErrorIs(t, err, reflection.ErrProposalNotFound)
}

func TestRepo_ListByTenant_Filters(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := reflection.NewRepo(pg)

	p1, err := repo.Insert(ctx, newProp(tid, uid, "a", 0.5))
	require.NoError(t, err)
	_, err = repo.Insert(ctx, newProp(tid, uid, "b", 0.6))
	require.NoError(t, err)
	// Auto-approved one: insert directly in approved status.
	p3 := newProp(tid, uid, "c", 0.95)
	p3.Status = reflection.StatusAutoApproved
	memID := uuid.New()
	p3.MemoryID = &memID
	_, err = repo.Insert(ctx, p3)
	require.NoError(t, err)

	all, err := repo.ListByTenant(ctx, tid, reflection.ListFilter{})
	require.NoError(t, err)
	require.Len(t, all, 3)

	pending, err := repo.ListByTenant(ctx, tid, reflection.ListFilter{Status: reflection.StatusPending})
	require.NoError(t, err)
	require.Len(t, pending, 2)

	auto, err := repo.ListByTenant(ctx, tid, reflection.ListFilter{Status: reflection.StatusAutoApproved})
	require.NoError(t, err)
	require.Len(t, auto, 1)
	require.Equal(t, "c", auto[0].Content)

	byOwner, err := repo.ListByTenant(ctx, tid, reflection.ListFilter{OwnerUserID: &uid})
	require.NoError(t, err)
	require.Len(t, byOwner, 3)

	other := uuid.New()
	byOther, err := repo.ListByTenant(ctx, tid, reflection.ListFilter{OwnerUserID: &other})
	require.NoError(t, err)
	require.Empty(t, byOther)

	// Bad status filter
	_, err = repo.ListByTenant(ctx, tid, reflection.ListFilter{Status: "bogus"})
	require.ErrorIs(t, err, reflection.ErrInvalidStatus)

	// Cross-tenant isolation
	otherTenant := uuid.New()
	cross, err := repo.ListByTenant(ctx, otherTenant, reflection.ListFilter{})
	require.NoError(t, err)
	require.Empty(t, cross)

	_ = p1
}

func TestRepo_MarkDecided_ApproveThenIdempotent(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := reflection.NewRepo(pg)

	p, err := repo.Insert(ctx, newProp(tid, uid, "x", 0.5))
	require.NoError(t, err)

	memID := uuid.New()
	decider := uid
	approved, err := repo.MarkDecided(ctx, tid, p.ID, reflection.StatusApproved, &memID, &decider)
	require.NoError(t, err)
	require.Equal(t, reflection.StatusApproved, approved.Status)
	require.NotNil(t, approved.MemoryID)
	require.Equal(t, memID, *approved.MemoryID)
	require.NotNil(t, approved.DecidedAt)
	require.NotNil(t, approved.DecidedBy)
	require.Equal(t, decider, *approved.DecidedBy)

	// Re-decide → ErrNotPending
	_, err = repo.MarkDecided(ctx, tid, p.ID, reflection.StatusRejected, nil, &decider)
	require.ErrorIs(t, err, reflection.ErrNotPending)
}

func TestRepo_MarkDecided_Reject(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := reflection.NewRepo(pg)

	p, err := repo.Insert(ctx, newProp(tid, uid, "x", 0.5))
	require.NoError(t, err)

	decider := uid
	rejected, err := repo.MarkDecided(ctx, tid, p.ID, reflection.StatusRejected, nil, &decider)
	require.NoError(t, err)
	require.Equal(t, reflection.StatusRejected, rejected.Status)
	require.Nil(t, rejected.MemoryID)
}

func TestRepo_MarkDecided_NotFound(t *testing.T) {
	pg := newPool(t)
	tid, _ := fixtures(t, pg)
	ctx := context.Background()
	repo := reflection.NewRepo(pg)

	_, err := repo.MarkDecided(ctx, tid, uuid.New(), reflection.StatusApproved, nil, nil)
	require.ErrorIs(t, err, reflection.ErrProposalNotFound)
}

func TestRepo_MarkDecided_CrossTenant(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := reflection.NewRepo(pg)

	p, err := repo.Insert(ctx, newProp(tid, uid, "x", 0.5))
	require.NoError(t, err)

	// Different tenant cannot decide this proposal.
	_, err = repo.MarkDecided(ctx, uuid.New(), p.ID, reflection.StatusApproved, nil, nil)
	require.ErrorIs(t, err, reflection.ErrProposalNotFound)
}
