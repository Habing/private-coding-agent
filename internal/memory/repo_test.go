package memory_test

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
	"github.com/yourorg/private-coding-agent/internal/memory"
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

func newMem(t *testing.T, tid, uid uuid.UUID, typ, content string, tags []string) *memory.Memory {
	t.Helper()
	return &memory.Memory{
		ID:          uuid.New(),
		TenantID:    tid,
		OwnerUserID: uid,
		Type:        typ,
		Content:     content,
		Tags:        tags,
		Source:      memory.SourceUser,
	}
}

func TestRepo_InsertGetList(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := memory.NewRepo(pg)

	m, err := repo.Insert(ctx, newMem(t, tid, uid, memory.TypePreference, "user likes Go", []string{"go", "lang"}))
	require.NoError(t, err)
	require.Equal(t, "user likes Go", m.Content)
	require.Equal(t, []string{"go", "lang"}, m.Tags)
	require.WithinDuration(t, time.Now(), m.CreatedAt, 10*time.Second)
	require.WithinDuration(t, time.Now(), m.LastUsedAt, 10*time.Second)

	got, err := repo.Get(ctx, tid, uid, m.ID)
	require.NoError(t, err)
	require.Equal(t, m.ID, got.ID)

	list, err := repo.List(ctx, tid, uid, memory.ListFilter{})
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestRepo_GetNotFound_CrossTenant(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := memory.NewRepo(pg)

	m, err := repo.Insert(ctx, newMem(t, tid, uid, memory.TypeKnowledge, "x", nil))
	require.NoError(t, err)

	_, err = repo.Get(ctx, uuid.New(), uid, m.ID)
	require.ErrorIs(t, err, memory.ErrMemoryNotFound)
	_, err = repo.Get(ctx, tid, uuid.New(), m.ID)
	require.ErrorIs(t, err, memory.ErrMemoryNotFound)
	_, err = repo.Get(ctx, tid, uid, uuid.New())
	require.ErrorIs(t, err, memory.ErrMemoryNotFound)
}

func TestRepo_List_Filters(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := memory.NewRepo(pg)

	_, err := repo.Insert(ctx, newMem(t, tid, uid, memory.TypePreference, "prefers tabs", []string{"style"}))
	require.NoError(t, err)
	_, err = repo.Insert(ctx, newMem(t, tid, uid, memory.TypeKnowledge, "uses postgres", []string{"db", "pg"}))
	require.NoError(t, err)
	_, err = repo.Insert(ctx, newMem(t, tid, uid, memory.TypeKnowledge, "loves Go", []string{"go"}))
	require.NoError(t, err)

	byType, err := repo.List(ctx, tid, uid, memory.ListFilter{Type: memory.TypeKnowledge})
	require.NoError(t, err)
	require.Len(t, byType, 2)

	byTag, err := repo.List(ctx, tid, uid, memory.ListFilter{Tags: []string{"pg"}})
	require.NoError(t, err)
	require.Len(t, byTag, 1)
	require.Equal(t, "uses postgres", byTag[0].Content)

	byQ, err := repo.List(ctx, tid, uid, memory.ListFilter{Query: "Go"})
	require.NoError(t, err)
	require.Len(t, byQ, 1)
}

func TestRepo_List_LimitOffset(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := memory.NewRepo(pg)

	for i := 0; i < 5; i++ {
		_, err := repo.Insert(ctx, newMem(t, tid, uid, memory.TypeKnowledge, fmt.Sprintf("n=%d", i), nil))
		require.NoError(t, err)
	}
	list, err := repo.List(ctx, tid, uid, memory.ListFilter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, list, 2)

	page, err := repo.List(ctx, tid, uid, memory.ListFilter{Limit: 2, Offset: 2})
	require.NoError(t, err)
	require.Len(t, page, 2)
}

func TestRepo_Update_Partial(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := memory.NewRepo(pg)

	m, err := repo.Insert(ctx, newMem(t, tid, uid, memory.TypePreference, "orig", []string{"a"}))
	require.NoError(t, err)

	newContent := "updated"
	updated, err := repo.Update(ctx, tid, uid, m.ID, memory.UpdateRequest{Content: &newContent})
	require.NoError(t, err)
	require.Equal(t, "updated", updated.Content)
	require.Equal(t, memory.TypePreference, updated.Type)
	require.Equal(t, []string{"a"}, updated.Tags)

	// Replace tags with empty slice (TagsSet=true, Tags=nil → clear).
	cleared, err := repo.Update(ctx, tid, uid, m.ID, memory.UpdateRequest{TagsSet: true})
	require.NoError(t, err)
	require.Equal(t, []string{}, cleared.Tags)

	// Cross-tenant update returns ErrMemoryNotFound.
	_, err = repo.Update(ctx, uuid.New(), uid, m.ID, memory.UpdateRequest{Content: &newContent})
	require.ErrorIs(t, err, memory.ErrMemoryNotFound)
}

func TestRepo_Delete(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := memory.NewRepo(pg)

	m, err := repo.Insert(ctx, newMem(t, tid, uid, memory.TypeKnowledge, "x", nil))
	require.NoError(t, err)
	require.NoError(t, repo.Delete(ctx, tid, uid, m.ID))

	_, err = repo.Get(ctx, tid, uid, m.ID)
	require.ErrorIs(t, err, memory.ErrMemoryNotFound)

	require.ErrorIs(t, repo.Delete(ctx, tid, uid, m.ID), memory.ErrMemoryNotFound)
}

func TestRepo_Search_VariousFilters(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := memory.NewRepo(pg)

	_, err := repo.Insert(ctx, newMem(t, tid, uid, memory.TypePreference, "user likes Go and tabs", []string{"go", "style"}))
	require.NoError(t, err)
	_, err = repo.Insert(ctx, newMem(t, tid, uid, memory.TypeKnowledge, "uses pgvector for embeddings", []string{"db", "ai"}))
	require.NoError(t, err)
	_, err = repo.Insert(ctx, newMem(t, tid, uid, memory.TypeLesson, "rate limit external APIs", []string{"infra"}))
	require.NoError(t, err)

	byQuery, err := repo.Search(ctx, tid, uid, memory.SearchRequest{Query: "Go"})
	require.NoError(t, err)
	require.Len(t, byQuery, 1)

	byTags, err := repo.Search(ctx, tid, uid, memory.SearchRequest{Tags: []string{"db", "infra"}})
	require.NoError(t, err)
	require.Len(t, byTags, 2)

	byType, err := repo.Search(ctx, tid, uid, memory.SearchRequest{Type: memory.TypeLesson})
	require.NoError(t, err)
	require.Len(t, byType, 1)

	combined, err := repo.Search(ctx, tid, uid, memory.SearchRequest{Query: "uses", Type: memory.TypeKnowledge})
	require.NoError(t, err)
	require.Len(t, combined, 1)
}

func TestRepo_Search_TouchesLastUsedAt(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := memory.NewRepo(pg)

	m, err := repo.Insert(ctx, newMem(t, tid, uid, memory.TypeKnowledge, "find me", []string{"x"}))
	require.NoError(t, err)
	original := m.LastUsedAt

	// Ensure DB clock advances enough to detect the touch.
	time.Sleep(50 * time.Millisecond)

	hits, err := repo.Search(ctx, tid, uid, memory.SearchRequest{Query: "find"})
	require.NoError(t, err)
	require.Len(t, hits, 1)

	got, err := repo.Get(ctx, tid, uid, m.ID)
	require.NoError(t, err)
	require.True(t, got.LastUsedAt.After(original), "last_used_at should advance after search hit")
}

func TestRepo_Search_CrossTenant(t *testing.T) {
	pg := newPool(t)
	tidA, uidA := fixtures(t, pg)
	tidB, _ := fixtures(t, pg)
	ctx := context.Background()
	repo := memory.NewRepo(pg)

	_, err := repo.Insert(ctx, newMem(t, tidA, uidA, memory.TypeKnowledge, "tenant A secret", []string{"a"}))
	require.NoError(t, err)

	hitsB, err := repo.Search(ctx, tidB, uidA, memory.SearchRequest{Query: "tenant"})
	require.NoError(t, err)
	require.Len(t, hitsB, 0)
}
