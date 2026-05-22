package mcp_test

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/db"
	"github.com/yourorg/private-coding-agent/internal/mcp"
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

func sampleServer(tenantID uuid.UUID, slug string) *mcp.Server {
	return &mcp.Server{
		TenantID:    tenantID,
		Slug:        slug,
		Name:        "Sample MCP",
		Description: "for tests",
		URL:         "http://example.local/mcp",
		AuthType:    mcp.AuthTypeBearer,
		AuthToken:   "tok-" + slug,
		Headers:     map[string]string{"X-Tenant": "acme"},
		Enabled:     true,
	}
}

func TestRepo_CRUD(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	ctx := context.Background()
	tid := seedTenant(t, p)

	created, err := repo.Insert(ctx, sampleServer(tid, "alpha"))
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, created.ID)
	assert.Equal(t, tid, created.TenantID)
	assert.Equal(t, "alpha", created.Slug)
	assert.Equal(t, mcp.TransportHTTP, created.Transport)
	assert.Equal(t, mcp.AuthTypeBearer, created.AuthType)
	assert.Equal(t, "tok-alpha", created.AuthToken)
	assert.Equal(t, "acme", created.Headers["X-Tenant"])
	assert.Empty(t, created.ToolsCache)

	// Get by id
	got, err := repo.Get(ctx, tid, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "alpha", got.Slug)

	// GetBySlug
	got2, err := repo.GetBySlug(ctx, tid, "alpha")
	require.NoError(t, err)
	assert.Equal(t, created.ID, got2.ID)

	// Update name + disable
	newName := "Renamed"
	disabled := false
	updated, err := repo.Update(ctx, tid, created.ID,
		&newName, nil, nil, nil, nil, nil, &disabled)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	assert.False(t, updated.Enabled)

	// Delete
	require.NoError(t, repo.Delete(ctx, tid, created.ID))
	_, err = repo.Get(ctx, tid, created.ID)
	assert.ErrorIs(t, err, mcp.ErrServerNotFound)
}

func TestRepo_SlugConflict(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	ctx := context.Background()
	tid := seedTenant(t, p)

	_, err := repo.Insert(ctx, sampleServer(tid, "dup"))
	require.NoError(t, err)

	_, err = repo.Insert(ctx, sampleServer(tid, "dup"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, mcp.ErrSlugConflict),
		"expected ErrSlugConflict, got %v", err)
}

func TestRepo_TenantIsolation(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	ctx := context.Background()
	tA := seedTenant(t, p)
	tB := seedTenant(t, p)

	// Same slug across two tenants is allowed.
	_, err := repo.Insert(ctx, sampleServer(tA, "shared"))
	require.NoError(t, err)
	_, err = repo.Insert(ctx, sampleServer(tB, "shared"))
	require.NoError(t, err)

	listA, err := repo.List(ctx, tA)
	require.NoError(t, err)
	require.Len(t, listA, 1)
	assert.Equal(t, tA, listA[0].TenantID)

	listB, err := repo.List(ctx, tB)
	require.NoError(t, err)
	require.Len(t, listB, 1)
	assert.Equal(t, tB, listB[0].TenantID)

	// Cross-tenant GetBySlug must miss.
	srvA, err := repo.GetBySlug(ctx, tA, "shared")
	require.NoError(t, err)
	_, err = repo.Get(ctx, tB, srvA.ID)
	assert.ErrorIs(t, err, mcp.ErrServerNotFound)
}

func TestRepo_ToolsCacheRoundTrip(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	ctx := context.Background()
	tid := seedTenant(t, p)

	created, err := repo.Insert(ctx, sampleServer(tid, "cache"))
	require.NoError(t, err)

	tools := []mcp.ToolSchema{
		{
			Name:        "echo",
			Description: "echoes input",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"text": map[string]any{"type": "string"}},
			},
			Annotations: map[string]any{"destructiveHint": false},
		},
		{
			Name:        "wipe",
			InputSchema: map[string]any{"type": "object"},
			Annotations: map[string]any{"destructiveHint": true},
		},
	}
	require.NoError(t, repo.UpdateToolsCache(ctx, tid, created.ID, tools, time.Now()))

	got, err := repo.Get(ctx, tid, created.ID)
	require.NoError(t, err)
	require.Len(t, got.ToolsCache, 2)
	assert.Equal(t, "echo", got.ToolsCache[0].Name)
	assert.Equal(t, false, got.ToolsCache[0].Annotations["destructiveHint"])
	assert.Equal(t, "wipe", got.ToolsCache[1].Name)
	assert.Equal(t, true, got.ToolsCache[1].Annotations["destructiveHint"])
	assert.NotNil(t, got.LastSeenAt)
	assert.Empty(t, got.LastError)
}

func TestRepo_UpdateLastErrorAndSeen(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	ctx := context.Background()
	tid := seedTenant(t, p)
	created, err := repo.Insert(ctx, sampleServer(tid, "err"))
	require.NoError(t, err)

	require.NoError(t, repo.UpdateLastError(ctx, tid, created.ID, "connection refused"))
	got, err := repo.Get(ctx, tid, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "connection refused", got.LastError)
	assert.Nil(t, got.LastSeenAt)

	now := time.Now()
	require.NoError(t, repo.UpdateLastSeen(ctx, tid, created.ID, now))
	got, err = repo.Get(ctx, tid, created.ID)
	require.NoError(t, err)
	assert.Empty(t, got.LastError)
	require.NotNil(t, got.LastSeenAt)
}

func TestRepo_ListAllEnabled_FiltersDisabled(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	ctx := context.Background()
	tid := seedTenant(t, p)

	onSrv := sampleServer(tid, "lae-on")
	onSrv.Enabled = true
	offSrv := sampleServer(tid, "lae-off")
	offSrv.Enabled = false
	_, err := repo.Insert(ctx, onSrv)
	require.NoError(t, err)
	_, err = repo.Insert(ctx, offSrv)
	require.NoError(t, err)

	all, err := repo.ListAllEnabled(ctx)
	require.NoError(t, err)
	slugs := map[string]bool{}
	for _, s := range all {
		if s.TenantID == tid {
			slugs[s.Slug] = true
		}
	}
	assert.True(t, slugs["lae-on"])
	assert.False(t, slugs["lae-off"])
}

func TestRepo_ValidateSlug(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"echo", true},
		{"my-server", true},
		{"a_b-c-9", true},
		{"a", true},
		{"", false},
		{"Echo", false},          // uppercase
		{"-leading", false},      // can't start with dash
		{"with space", false},
		{"with.dot", false},
	}
	for _, c := range cases {
		err := mcp.ValidateSlug(c.in)
		if c.ok {
			assert.NoError(t, err, "expected %q valid", c.in)
		} else {
			assert.ErrorIs(t, err, mcp.ErrSlugInvalid, "expected %q invalid", c.in)
		}
	}
}
