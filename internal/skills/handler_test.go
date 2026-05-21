package skills_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/skills"
)

func newHandlerRouter(t *testing.T, reg *skills.Registry) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	tok, _ := j.Issue(uuid.New(), uuid.New(), "member")
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	skills.NewHandler(reg).Register(g)
	return r, "Bearer " + tok
}

func mkSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	sub := filepath.Join(dir, name)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	full := "---\nname: " + name + "\ndescription: " + name + " skill\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(sub, "SKILL.md"), []byte(full), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestHandler_List_Empty(t *testing.T) {
	r, tok := newHandlerRouter(t, skills.NewRegistry())
	req := httptest.NewRequest(http.MethodGet, "/skills", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var out struct {
		Skills []skills.SkillMeta `json:"skills"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Empty(t, out.Skills)
}

func TestHandler_List_TwoSkillsSorted(t *testing.T) {
	td := t.TempDir()
	mkSkill(t, td, "zeta", "body z")
	mkSkill(t, td, "alpha", "body a")
	reg := skills.NewRegistry()
	n, errs := reg.LoadFromDirs([]string{td})
	require.Equal(t, 2, n)
	require.Empty(t, errs)

	r, tok := newHandlerRouter(t, reg)
	req := httptest.NewRequest(http.MethodGet, "/skills", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var out struct {
		Skills []skills.SkillMeta `json:"skills"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Len(t, out.Skills, 2)
	require.Equal(t, "alpha", out.Skills[0].ID)
	require.Equal(t, "zeta", out.Skills[1].ID)
}

func TestHandler_Get_NotFound(t *testing.T) {
	r, tok := newHandlerRouter(t, skills.NewRegistry())
	req := httptest.NewRequest(http.MethodGet, "/skills/missing", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_Get_WithoutBody(t *testing.T) {
	td := t.TempDir()
	mkSkill(t, td, "alpha", "body content")
	reg := skills.NewRegistry()
	reg.LoadFromDirs([]string{td})
	r, tok := newHandlerRouter(t, reg)
	req := httptest.NewRequest(http.MethodGet, "/skills/alpha", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Equal(t, "alpha", out["id"])
	require.NotContains(t, out, "body")
}

func TestHandler_Get_IncludeBody(t *testing.T) {
	td := t.TempDir()
	mkSkill(t, td, "alpha", "body content")
	reg := skills.NewRegistry()
	reg.LoadFromDirs([]string{td})
	r, tok := newHandlerRouter(t, reg)
	req := httptest.NewRequest(http.MethodGet, "/skills/alpha?include=body", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Equal(t, "alpha", out["id"])
	require.Contains(t, out["body"].(string), "body content")
}

func TestHandler_Unauthorized(t *testing.T) {
	r, _ := newHandlerRouter(t, skills.NewRegistry())
	req := httptest.NewRequest(http.MethodGet, "/skills", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
