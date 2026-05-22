package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/mcp"
)

// memSink is a tiny audit sink that stores entries in memory so handler tests
// can assert audit emissions without a Postgres dependency. Concurrent-safe via
// channel; tests read with collect().
type memSink struct{ ch chan audit.Entry }

func newMemSink() *memSink { return &memSink{ch: make(chan audit.Entry, 64)} }

func (s *memSink) Append(_ context.Context, e audit.Entry) error {
	s.ch <- e
	return nil
}

func (s *memSink) collect(t *testing.T) []audit.Entry {
	t.Helper()
	close(s.ch)
	out := []audit.Entry{}
	for e := range s.ch {
		out = append(out, e)
	}
	return out
}

// withClaims wires an in-memory auth context onto Gin so handlers see a
// tenant + admin user. Mirrors how auth.Middleware injects ctxKey but skips
// JWT parsing.
func withClaims(tid, uid uuid.UUID, role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := auth.WithClaims(c.Request.Context(), &auth.Claims{
			TenantID: tid, UserID: uid, Role: role,
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func mountHandler(t *testing.T, mgr *mcp.Manager, repo *mcp.Repo, sink audit.Sink, tid, uid uuid.UUID) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	rg := r.Group("/")
	rg.Use(withClaims(tid, uid, "admin"))
	mcp.NewAdminHandler(mgr, repo, sink).Register(rg)
	return r
}

func doJSON(t *testing.T, r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHandler_Create_RedactsTokenAndAudits(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	sink := newMemSink()
	mgr := mcp.NewManager(repo, bus, sink, defaultCfg())
	tid := seedTenant(t, p)
	uid := uuid.New()
	r := mountHandler(t, mgr, repo, sink, tid, uid)

	srv := newMockMCPServer(t, []mcp.ToolSchema{
		{Name: "echo", InputSchema: map[string]any{"type": "object"}},
	})

	w := doJSON(t, r, http.MethodPost, "/admin/mcp-servers", map[string]any{
		"slug": "h1", "name": "H1", "url": srv.URL,
		"auth_type": "bearer", "auth_token": "super-secret",
		"enabled": true,
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var got mcp.Server
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "h1", got.Slug)
	assert.Equal(t, "***", got.AuthToken, "auth_token must be redacted in response")
	assert.GreaterOrEqual(t, len(got.ToolsCache), 1, "tools_cache should be populated on success")

	// DB still has the real token.
	row, err := repo.Get(context.Background(), tid, got.ID)
	require.NoError(t, err)
	assert.Equal(t, "super-secret", row.AuthToken)

	entries := sink.collect(t)
	require.Len(t, entries, 1)
	assert.Equal(t, "mcp.admin.create", entries[0].Action)
	assert.Equal(t, "h1", entries[0].Target)
	assert.NotContains(t, entries[0].Metadata["token_fp"], "super-secret",
		"audit must store a fingerprint, not the raw token")
}

func TestHandler_Create_RejectsBadSlug(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	mgr := mcp.NewManager(repo, bus, nil, defaultCfg())
	tid := seedTenant(t, p)
	r := mountHandler(t, mgr, repo, nil, tid, uuid.New())

	w := doJSON(t, r, http.MethodPost, "/admin/mcp-servers", map[string]any{
		"slug": "BAD SLUG", "name": "x", "url": "http://x",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestHandler_Create_SlugConflictReturns409(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	mgr := mcp.NewManager(repo, bus, nil, defaultCfg())
	tid := seedTenant(t, p)
	r := mountHandler(t, mgr, repo, nil, tid, uuid.New())

	srv := newMockMCPServer(t, nil)
	body := map[string]any{"slug": "dup", "name": "x", "url": srv.URL}
	w := doJSON(t, r, http.MethodPost, "/admin/mcp-servers", body)
	require.Equal(t, http.StatusCreated, w.Code)

	w = doJSON(t, r, http.MethodPost, "/admin/mcp-servers", body)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHandler_Get_CrossTenantReturns404(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	mgr := mcp.NewManager(repo, bus, nil, defaultCfg())
	tA := seedTenant(t, p)
	tB := seedTenant(t, p)

	created, err := repo.Insert(context.Background(), sampleServer(tA, "x1"))
	require.NoError(t, err)

	// Mount with tenant B claims.
	r := mountHandler(t, mgr, repo, nil, tB, uuid.New())
	w := doJSON(t, r, http.MethodGet, "/admin/mcp-servers/"+created.ID.String(), nil)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestHandler_List_RedactsTokens(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	mgr := mcp.NewManager(repo, bus, nil, defaultCfg())
	tid := seedTenant(t, p)
	r := mountHandler(t, mgr, repo, nil, tid, uuid.New())

	s := sampleServer(tid, "list-redact")
	s.AuthType = mcp.AuthTypeBearer
	s.AuthToken = "must-not-leak"
	_, err := repo.Insert(context.Background(), s)
	require.NoError(t, err)

	w := doJSON(t, r, http.MethodGet, "/admin/mcp-servers", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "must-not-leak",
		"list response must redact bearer tokens")
	assert.Contains(t, w.Body.String(), `"***"`)
}

func TestHandler_Delete_RemovesRowAndAudits(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	sink := newMemSink()
	mgr := mcp.NewManager(repo, bus, sink, defaultCfg())
	tid := seedTenant(t, p)
	r := mountHandler(t, mgr, repo, sink, tid, uuid.New())

	srv := newMockMCPServer(t, []mcp.ToolSchema{
		{Name: "echo", InputSchema: map[string]any{"type": "object"}},
	})
	created, err := repo.Insert(context.Background(), sampleServerWithURL(tid, "del", srv.URL))
	require.NoError(t, err)

	// Make it live on the Bus first.
	_, err = mgr.RegisterServer(context.Background(), created)
	require.NoError(t, err)
	assert.True(t, mgr.IsRegistered("mcp.del.echo"))

	w := doJSON(t, r, http.MethodDelete, "/admin/mcp-servers/"+created.ID.String(), nil)
	assert.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	_, err = repo.Get(context.Background(), tid, created.ID)
	assert.ErrorIs(t, err, mcp.ErrServerNotFound)
	assert.False(t, mgr.IsRegistered("mcp.del.echo"), "delete must unregister Bus entries")

	entries := sink.collect(t)
	var del *audit.Entry
	for i := range entries {
		if entries[i].Action == "mcp.admin.delete" {
			del = &entries[i]
		}
	}
	require.NotNil(t, del, "expected mcp.admin.delete audit entry")
	assert.Equal(t, "del", del.Target)
}

func TestHandler_Disabled_503(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	// nil manager simulates cfg.MCP.Enabled=false
	r := mountHandler(t, nil, repo, nil, seedTenant(t, p), uuid.New())
	w := doJSON(t, r, http.MethodGet, "/admin/mcp-servers", nil)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
}

func TestHandler_Refresh_ReturnsToolList(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	mgr := mcp.NewManager(repo, bus, nil, defaultCfg())
	tid := seedTenant(t, p)
	r := mountHandler(t, mgr, repo, nil, tid, uuid.New())

	srv := newMockMCPServer(t, []mcp.ToolSchema{
		{Name: "echo", InputSchema: map[string]any{"type": "object"}},
	})
	created, err := repo.Insert(context.Background(), sampleServerWithURL(tid, "rfr", srv.URL))
	require.NoError(t, err)

	w := doJSON(t, r, http.MethodPost,
		"/admin/mcp-servers/"+created.ID.String()+"/refresh", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct{ Tools []mcp.ToolSchema }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Tools, 1)
	assert.Equal(t, "echo", resp.Tools[0].Name)
}

func TestHandler_Test_ProbesWithoutPersistence(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	mgr := mcp.NewManager(repo, bus, nil, defaultCfg())
	tid := seedTenant(t, p)
	r := mountHandler(t, mgr, repo, nil, tid, uuid.New())

	srv := newMockMCPServer(t, []mcp.ToolSchema{
		{Name: "echo", InputSchema: map[string]any{"type": "object"}},
	})
	// Test by body (no :id provided).
	w := doJSON(t, r, http.MethodPost, "/admin/mcp-servers/"+uuid.New().String()+"/test",
		map[string]any{"url": srv.URL, "auth_type": "none"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		OK bool `json:"ok"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.OK)
}

func TestHandler_EnableDisable_RoundTrip(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	mgr := mcp.NewManager(repo, bus, nil, defaultCfg())
	tid := seedTenant(t, p)
	r := mountHandler(t, mgr, repo, nil, tid, uuid.New())

	srv := newMockMCPServer(t, []mcp.ToolSchema{
		{Name: "echo", InputSchema: map[string]any{"type": "object"}},
	})
	created, err := repo.Insert(context.Background(), sampleServerWithURL(tid, "ed", srv.URL))
	require.NoError(t, err)

	// Enable first → register
	w := doJSON(t, r, http.MethodPost,
		"/admin/mcp-servers/"+created.ID.String()+"/enable", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, mgr.IsRegistered("mcp.ed.echo"))

	// Disable → unregister
	w = doJSON(t, r, http.MethodPost,
		"/admin/mcp-servers/"+created.ID.String()+"/disable", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, mgr.IsRegistered("mcp.ed.echo"))
}

// sampleServerWithURL is a convenience wrapper around sampleServer (defined in
// repo_test.go) that swaps the URL for an httptest endpoint.
func sampleServerWithURL(tid uuid.UUID, slug, url string) *mcp.Server {
	s := sampleServer(tid, slug)
	s.URL = url
	s.AuthType = mcp.AuthTypeNone
	s.AuthToken = ""
	return s
}
