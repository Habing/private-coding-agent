package skills_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/skills"
)

func newAdminRouter(t *testing.T, p *pgxpool.Pool, tenantID uuid.UUID, role string) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	tok, _ := j.Issue(uuid.New(), tenantID, role)
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	g.Use(auth.RequireAdmin())
	skills.NewAdminHandler(skills.NewDBRepo(p)).Register(g)
	return r, "Bearer " + tok
}

func TestAdminHandler_RejectsNonAdmin(t *testing.T) {
	p := newPool(t)
	tid := seedTenant(t, p)
	r, tok := newAdminRouter(t, p, tid, "member")
	req := httptest.NewRequest(http.MethodGet, "/admin/skills", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestAdminHandler_CRUDLifecycle(t *testing.T) {
	p := newPool(t)
	tid := seedTenant(t, p)
	r, tok := newAdminRouter(t, p, tid, "admin")

	// Create
	body, _ := json.Marshal(map[string]any{
		"skill_key": "tenant-marker",
		"description": "demo",
		"body": "TENANT BODY",
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/skills", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
	var created skills.DBSkill
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.Equal(t, "tenant-marker", created.SkillKey)
	require.True(t, created.Enabled)

	// Conflict
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/skills", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusConflict, w.Code)

	// Invalid key
	bad, _ := json.Marshal(map[string]any{"skill_key": "BadKey", "body": "x"})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/skills", bytes.NewReader(bad))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)

	// List (no body)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/skills", nil)
	req.Header.Set("Authorization", tok)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var listResp struct {
		Skills []skills.DBSkill `json:"skills"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	require.Len(t, listResp.Skills, 1)
	require.Empty(t, listResp.Skills[0].Body) // omitted unless ?include=body

	// List with body
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/skills?include=body", nil)
	req.Header.Set("Authorization", tok)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	require.Equal(t, "TENANT BODY", listResp.Skills[0].Body)

	// Get single
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/skills/tenant-marker", nil)
	req.Header.Set("Authorization", tok)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Update enabled
	updBody, _ := json.Marshal(map[string]any{"enabled": false})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/admin/skills/tenant-marker", bytes.NewReader(updBody))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var upd skills.DBSkill
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &upd))
	require.False(t, upd.Enabled)

	// Delete
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/admin/skills/tenant-marker", nil)
	req.Header.Set("Authorization", tok)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)

	// 404 after delete
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/skills/tenant-marker", nil)
	req.Header.Set("Authorization", tok)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestAdminHandler_ProfileBinding(t *testing.T) {
	p := newPool(t)
	tid := seedTenant(t, p)
	r, tok := newAdminRouter(t, p, tid, "admin")

	// Set binding
	body, _ := json.Marshal(map[string]any{"skill_keys": []string{"a-1", "b-2"}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/profiles/coding/skills", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Get
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/profiles/coding/skills", nil)
	req.Header.Set("Authorization", tok)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var got struct {
		Profile   string   `json:"profile"`
		SkillKeys []string `json:"skill_keys"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, []string{"a-1", "b-2"}, got.SkillKeys)

	// Invalid key
	bad, _ := json.Marshal(map[string]any{"skill_keys": []string{"BadKey"}})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/admin/profiles/coding/skills", bytes.NewReader(bad))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAdminHandler_TenantIsolation(t *testing.T) {
	p := newPool(t)
	tidA := seedTenant(t, p)
	tidB := seedTenant(t, p)

	rA, tokA := newAdminRouter(t, p, tidA, "admin")
	body, _ := json.Marshal(map[string]any{"skill_key": "tenant-secret", "body": "FROM A"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/skills", bytes.NewReader(body))
	req.Header.Set("Authorization", tokA)
	req.Header.Set("Content-Type", "application/json")
	rA.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	rB, tokB := newAdminRouter(t, p, tidB, "admin")
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/skills/tenant-secret", nil)
	req.Header.Set("Authorization", tokB)
	rB.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)

	// Counters
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/skills", nil)
	req.Header.Set("Authorization", tokB)
	rB.ServeHTTP(w, req)
	var lr struct {
		Skills []skills.DBSkill `json:"skills"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &lr))
	require.Empty(t, lr.Skills)
	_ = context.Background()
}
