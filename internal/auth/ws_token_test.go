package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// echoAuthRouter returns a gin engine that mounts WSTokenFromQuery in front of
// an echo handler returning whatever Authorization header reached it.
func echoAuthRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(auth.WSTokenFromQuery())
	r.GET("/echo", func(c *gin.Context) {
		c.String(http.StatusOK, c.GetHeader("Authorization"))
	})
	return r
}

func TestWSTokenFromQuery_HeaderTakesPrecedence(t *testing.T) {
	r := echoAuthRouter()
	req := httptest.NewRequest(http.MethodGet, "/echo?token=ignored", nil)
	req.Header.Set("Authorization", "Bearer real-header-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "Bearer real-header-token", w.Body.String(),
		"header should take precedence over ?token=")
}

func TestWSTokenFromQuery_QueryLifted(t *testing.T) {
	r := echoAuthRouter()
	req := httptest.NewRequest(http.MethodGet, "/echo?token=jwt-from-query", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "Bearer jwt-from-query", w.Body.String(),
		"missing header should be filled from ?token= as Bearer <t>")
}

func TestWSTokenFromQuery_Absent(t *testing.T) {
	r := echoAuthRouter()
	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "", w.Body.String(),
		"with neither header nor query the middleware must not block; downstream auth.Middleware handles 401")
}

func TestWSTokenFromQuery_PreservesSpecialChars(t *testing.T) {
	r := echoAuthRouter()
	// JWTs use base64url, but include "+", "/", "=" defensively in case a
	// non-standard token sneaks through. Encode them in the query so they
	// survive the URL parser; the middleware should hand the *decoded* value
	// straight through to the Bearer header.
	req := httptest.NewRequest(http.MethodGet, "/echo?token=ab%2Bcd%2Fef%3D", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "Bearer ab+cd/ef=", w.Body.String())
}

func TestWSTokenFromQuery_NotMounted_NoEffect(t *testing.T) {
	// Sanity: without the shim, a query-only request reaches the echo handler
	// with an empty Authorization header — confirming the shim is the thing
	// doing the work (not gin / not net/http).
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/echo", func(c *gin.Context) {
		c.String(http.StatusOK, c.GetHeader("Authorization"))
	})
	req := httptest.NewRequest(http.MethodGet, "/echo?token=x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, "", w.Body.String())
}
