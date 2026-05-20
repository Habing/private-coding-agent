package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/httpx"
)

func newSPAFixture(t *testing.T) (*gin.Engine, *fstest.MapFS) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Pre-register a JSON API route so we can assert fallback does NOT shadow it.
	r.GET("/api/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	// Also a POST so we can assert non-GET unmatched still 404s as JSON.
	r.POST("/api/echo", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"echo": true})
	})

	fsys := fstest.MapFS{
		"index.html":    {Data: []byte(`<html><body><div id="root"></div></body></html>`)},
		"assets/app.js": {Data: []byte(`console.log("ok")`)},
		"favicon.ico":   {Data: []byte("\x00\x00")},
	}
	require.NoError(t, httpx.RegisterSPAFallback(r, fsys))
	return r, &fsys
}

func get(r *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestSPAFallback_Root(t *testing.T) {
	r, _ := newSPAFixture(t)
	w := get(r, "/")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "text/html")
	require.Contains(t, w.Body.String(), `id="root"`)
}

func TestSPAFallback_UnknownClientRoute(t *testing.T) {
	r, _ := newSPAFixture(t)
	for _, path := range []string{"/login", "/sessions/abc-123", "/anything/nested/here"} {
		w := get(r, path)
		require.Equal(t, http.StatusOK, w.Code, "path %s", path)
		require.Contains(t, w.Body.String(), `id="root"`, "path %s should serve SPA shell", path)
	}
}

func TestSPAFallback_RealAsset(t *testing.T) {
	r, _ := newSPAFixture(t)
	w := get(r, "/assets/app.js")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `console.log`)

	wFav := get(r, "/favicon.ico")
	require.Equal(t, http.StatusOK, wFav.Code)
}

func TestSPAFallback_APIRouteNotShadowed(t *testing.T) {
	r, _ := newSPAFixture(t)
	w := get(r, "/api/ping")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")
	require.Contains(t, w.Body.String(), `"ok":true`)
}

func TestSPAFallback_NonGET_Returns404JSON(t *testing.T) {
	r, _ := newSPAFixture(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/some/spa/path", nil))
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")
	require.NotContains(t, w.Body.String(), `id="root"`)

	// PUT, DELETE also should not fall back.
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, "/x", nil))
		require.Equal(t, http.StatusNotFound, w.Code, "method %s", method)
	}
}

func TestSPAFallback_MissingIndexHTML(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	empty := fstest.MapFS{} // no index.html
	err := httpx.RegisterSPAFallback(r, empty)
	require.Error(t, err, "must refuse to register when index.html is missing")
}
