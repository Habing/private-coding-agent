package connectors_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/connectors"
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
	connectors.NewAdminHandler(nil, true).Register(rg)

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

func TestCatalogHandler_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	connectors.NewAdminHandler(nil, false).Register(r.Group("/"))

	req := httptest.NewRequest(http.MethodGet, "/admin/connectors/catalog", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
