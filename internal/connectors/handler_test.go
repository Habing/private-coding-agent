package connectors_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/connectors"
	tools "github.com/yourorg/private-coding-agent/internal/toolbus/tools"
)

func withClaims(tid, uid uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := auth.WithClaims(c.Request.Context(), &auth.Claims{
			TenantID: tid, UserID: uid, Role: "admin",
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func TestCatalogHandler_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tid := uuid.New()
	r := gin.New()
	rg := r.Group("/")
	rg.Use(withClaims(tid, uuid.New()))
	fetch := tools.NewHTTPFetch(tools.HTTPFetchConfig{
		Enabled: true, AllowHosts: []string{"mock-provider"},
	})
	connectors.NewAdminHandler(nil, fetch, nil, false).Register(rg)

	req := httptest.NewRequest(http.MethodGet, "/admin/connectors/catalog", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Recipes []struct {
			ID        string `json:"id"`
			Installed bool   `json:"installed"`
			Tools     []string `json:"tools"`
		} `json:"recipes"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.NotEmpty(t, body.Recipes)
	var httpFetch bool
	for _, rec := range body.Recipes {
		if rec.ID == "http-fetch" {
			httpFetch = rec.Installed
			require.Contains(t, rec.Tools, "http.fetch")
		}
	}
	require.True(t, httpFetch)
}

func TestCatalogHandler_ToolsNeverNull(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tid := uuid.New()
	r := gin.New()
	rg := r.Group("/")
	rg.Use(withClaims(tid, uuid.New()))
	connectors.NewAdminHandler(nil, nil, nil, false).Register(rg)

	req := httptest.NewRequest(http.MethodGet, "/admin/connectors/catalog", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), `"tools":null`)
}

func TestCatalogHandler_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	connectors.NewAdminHandler(nil, nil, nil, false).Register(r.Group("/"))

	req := httptest.NewRequest(http.MethodGet, "/admin/connectors/catalog", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHTTPFetchSettings_UpdateRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tid := uuid.New()
	fetch := tools.NewHTTPFetch(tools.HTTPFetchConfig{
		Enabled: true, AllowHosts: []string{"mock-provider"},
	})
	r := gin.New()
	rg := r.Group("/")
	rg.Use(withClaims(tid, uuid.New()))
	connectors.NewAdminHandler(nil, fetch, nil, true).Register(rg)

	body, _ := json.Marshal(map[string]any{
		"allow_hosts": []string{"*.baidu.com", "top.baidu.com"},
	})
	req := httptest.NewRequest(http.MethodPut, "/admin/connectors/http-fetch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"*.baidu.com", "top.baidu.com"}, fetch.AllowHosts())
}
