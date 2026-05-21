package session_test

import (
	"context"
	"encoding/json"
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
	"github.com/yourorg/private-coding-agent/internal/session"
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

// fixtures inserts a tenant + user and returns their UUIDs. Each call creates
// fresh rows so tests don't interfere with each other.
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

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pg, err := pgxpool.New(context.Background(), testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)
	return pg
}

func TestSessionRepo_CreateGetList(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := session.NewSessionRepo(pg)

	s := &session.Session{
		ID:          uuid.New(),
		TenantID:    tid,
		OwnerUserID: uid,
		Title:       "hello",
		Model:       "default-mock:gpt-4o",
		Profile:     "coding",
		Status:      session.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, s))

	got, err := repo.Get(ctx, tid, uid, s.ID)
	require.NoError(t, err)
	require.Equal(t, s.ID, got.ID)
	require.Equal(t, "hello", got.Title)
	require.Equal(t, session.StatusActive, got.Status)
	require.WithinDuration(t, time.Now(), got.CreatedAt, 10*time.Second)

	list, err := repo.List(ctx, tid, uid)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, s.ID, list[0].ID)
}

func TestSessionRepo_GetNotFound_CrossTenant(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := session.NewSessionRepo(pg)

	s := &session.Session{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Model: "m", Profile: "coding", Status: session.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, s))

	// Cross-tenant
	_, err := repo.Get(ctx, uuid.New(), uid, s.ID)
	require.ErrorIs(t, err, session.ErrSessionNotFound)
	// Cross-owner same tenant
	_, err = repo.Get(ctx, tid, uuid.New(), s.ID)
	require.ErrorIs(t, err, session.ErrSessionNotFound)
	// Truly missing id
	_, err = repo.Get(ctx, tid, uid, uuid.New())
	require.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestSessionRepo_Archive(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := session.NewSessionRepo(pg)

	s := &session.Session{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Model: "m", Profile: "coding", Status: session.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, s))
	require.NoError(t, repo.Archive(ctx, tid, uid, s.ID))

	got, err := repo.Get(ctx, tid, uid, s.ID)
	require.NoError(t, err)
	require.Equal(t, session.StatusArchived, got.Status)

	// Missing id returns ErrSessionNotFound.
	err = repo.Archive(ctx, tid, uid, uuid.New())
	require.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestMessageRepo_AppendAndList(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	srepo := session.NewSessionRepo(pg)
	mrepo := session.NewMessageRepo(pg)

	s := &session.Session{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Model: "m", Profile: "coding", Status: session.StatusActive,
	}
	require.NoError(t, srepo.Create(ctx, s))

	seq1, err := mrepo.NextSeq(ctx, s.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), seq1)

	require.NoError(t, mrepo.Append(ctx, &session.Message{
		ID: uuid.New(), SessionID: s.ID, TenantID: tid, Seq: seq1,
		Role: session.RoleUser, Content: "hi",
	}))

	seq2, err := mrepo.NextSeq(ctx, s.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), seq2)

	require.NoError(t, mrepo.Append(ctx, &session.Message{
		ID: uuid.New(), SessionID: s.ID, TenantID: tid, Seq: seq2,
		Role:      session.RoleAssistant,
		Content:   "hello",
		ToolCalls: json.RawMessage(`[{"id":"c1","type":"function"}]`),
		Metadata:  json.RawMessage(`{"kind":"assistant_message"}`),
	}))

	list, err := mrepo.List(ctx, tid, s.ID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, int64(1), list[0].Seq)
	require.Equal(t, "hi", list[0].Content)
	require.Equal(t, int64(2), list[1].Seq)
	require.Equal(t, session.RoleAssistant, list[1].Role)
	require.Contains(t, string(list[1].ToolCalls), `"c1"`)
	require.Contains(t, string(list[1].Metadata), `"assistant_message"`)
}

func TestMessageRepo_SeqUnique(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	srepo := session.NewSessionRepo(pg)
	mrepo := session.NewMessageRepo(pg)

	s := &session.Session{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Model: "m", Profile: "coding", Status: session.StatusActive,
	}
	require.NoError(t, srepo.Create(ctx, s))
	require.NoError(t, mrepo.Append(ctx, &session.Message{
		ID: uuid.New(), SessionID: s.ID, TenantID: tid, Seq: 1, Role: session.RoleUser, Content: "a",
	}))
	err := mrepo.Append(ctx, &session.Message{
		ID: uuid.New(), SessionID: s.ID, TenantID: tid, Seq: 1, Role: session.RoleUser, Content: "b",
	})
	require.Error(t, err, "duplicate seq must be rejected by UNIQUE constraint")
}

func TestMessageRepo_List_CrossTenant(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	srepo := session.NewSessionRepo(pg)
	mrepo := session.NewMessageRepo(pg)

	s := &session.Session{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Model: "m", Profile: "coding", Status: session.StatusActive,
	}
	require.NoError(t, srepo.Create(ctx, s))
	require.NoError(t, mrepo.Append(ctx, &session.Message{
		ID: uuid.New(), SessionID: s.ID, TenantID: tid, Seq: 1, Role: session.RoleUser, Content: "x",
	}))

	list, err := mrepo.List(ctx, uuid.New(), s.ID)
	require.NoError(t, err)
	require.Len(t, list, 0)
}
