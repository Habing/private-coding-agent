package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/httpx"
)

func TestHealthz(t *testing.T) {
	r := httpx.NewEngine(httpx.Deps{Ready: func() bool { return true }})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"ok"`)
}

func TestReadyz_NotReady(t *testing.T) {
	r := httpx.NewEngine(httpx.Deps{Ready: func() bool { return false }})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestReadyz_Ready(t *testing.T) {
	r := httpx.NewEngine(httpx.Deps{Ready: func() bool { return true }})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, w.Code)
}
